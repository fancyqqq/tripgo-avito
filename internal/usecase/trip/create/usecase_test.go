package create_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
	"github.com/fancyqqq/tripgo-avito/internal/model"
	"github.com/fancyqqq/tripgo-avito/internal/usecase/trip/create"
)

var (
	now     = time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	errDB   = errors.New("db is down")
	fixedAt = func() time.Time { return now }
)

func validInput() model.CreateTrip {
	return model.CreateTrip{
		UserID:   uuid.New(),
		DriverID: uuid.New(),
		Start:    trip.Point{Latitude: 55.75, Longitude: 37.61},
		End:      trip.Point{Latitude: 55.80, Longitude: 37.70},
		Price:    500,
	}
}

func runInTx(tx *MocktxManager) {
	tx.EXPECT().
		Do(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		})
}

func TestExecute(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   func() model.CreateTrip
		setup   func(repo *MocktripRepository, tx *MocktxManager)
		wantErr error
	}{
		{
			name:  "success",
			input: validInput,
			setup: func(repo *MocktripRepository, tx *MocktxManager) {
				runInTx(tx)
				gomock.InOrder(
					repo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil),
					repo.EXPECT().AddStatusChange(gomock.Any(), gomock.Any()).Return(nil),
				)
			},
		},
		{
			name:  "driver busy",
			input: validInput,
			setup: func(repo *MocktripRepository, tx *MocktxManager) {
				runInTx(tx)
				repo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(trip.ErrDriverBusy)
			},
			wantErr: trip.ErrDriverBusy,
		},
		{
			name:  "history write fails",
			input: validInput,
			setup: func(repo *MocktripRepository, tx *MocktxManager) {
				runInTx(tx)
				repo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
				repo.EXPECT().AddStatusChange(gomock.Any(), gomock.Any()).Return(errDB)
			},
			wantErr: errDB,
		},
		{
			name:  "transaction fails",
			input: validInput,
			setup: func(_ *MocktripRepository, tx *MocktxManager) {
				tx.EXPECT().Do(gomock.Any(), gomock.Any()).Return(errDB)
			},
			wantErr: errDB,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			repo := NewMocktripRepository(ctrl)
			tx := NewMocktxManager(ctrl)
			metrics := NewMocktripMetrics(ctrl)
			tt.setup(repo, tx)
			if tt.wantErr == nil {
				metrics.EXPECT().TripCreated(gomock.Any())
			}

			got, err := create.New(repo, nil, tx, metrics, fixedAt).Execute(context.Background(), tt.input())

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}
				if got != (model.CreateTripResult{}) {
					t.Fatalf("expected zero trip on error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestExecuteSavesTripAndHistory(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	repo := NewMocktripRepository(ctrl)
	tx := NewMocktxManager(ctrl)
	metrics := NewMocktripMetrics(ctrl)
	in := validInput()

	var saved trip.Trip
	var change trip.StatusChange

	runInTx(tx)
	repo.EXPECT().Create(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, tr trip.Trip) error {
			saved = tr
			return nil
		})
	repo.EXPECT().AddStatusChange(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, c trip.StatusChange) error {
			change = c
			return nil
		})
	metrics.EXPECT().TripCreated(gomock.Any())

	got, err := create.New(repo, nil, tx, metrics, fixedAt).Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := trip.Trip{
		ID:         got.ID,
		UserID:     in.UserID,
		DriverID:   in.DriverID,
		StartPoint: in.Start,
		EndPoint:   in.End,
		Price:      in.Price,
		Status:     trip.StatusActive,
		StartedAt:  now,
	}
	if got.ID == uuid.Nil {
		t.Fatal("trip id is empty")
	}
	if got.Trip != want {
		t.Fatalf("trip = %+v, want %+v", got, want)
	}
	if saved != got.Trip {
		t.Fatalf("saved trip = %+v, returned %+v", saved, got)
	}

	if change.TripID != got.ID {
		t.Fatalf("history trip id = %v, want %v", change.TripID, got.ID)
	}
	if change.FromStatus != nil {
		t.Fatalf("history from status = %q, want nil", *change.FromStatus)
	}
	if change.ToStatus != trip.StatusActive {
		t.Fatalf("history to status = %q, want %q", change.ToStatus, trip.StatusActive)
	}
	if change.Reason == "" {
		t.Fatal("history reason is empty")
	}
}

func TestExecuteValidationErrorSkipsRepository(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	repo := NewMocktripRepository(ctrl)
	tx := NewMocktxManager(ctrl)

	metrics := NewMocktripMetrics(ctrl)
	in := validInput()
	in.Price = -1

	_, err := create.New(repo, nil, tx, metrics, fixedAt).Execute(context.Background(), in)

	var vErr *trip.ValidationError
	if !errors.As(err, &vErr) {
		t.Fatalf("expected ValidationError, got %v", err)
	}
	if vErr.Field != "price" {
		t.Fatalf("field = %q, want %q", vErr.Field, "price")
	}
}

func TestExecuteIdempotency(t *testing.T) {
	t.Parallel()
	const replayCase = "replay"
	const claimErrorCase = "claim error"
	const completeErrorCase = "complete error"
	const readErrorCase = "read error"
	const claimCase = "claim"
	for _, name := range []string{claimCase, replayCase, "conflict", claimErrorCase, completeErrorCase, readErrorCase} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			repo := NewMocktripRepository(ctrl)
			tx := NewMocktxManager(ctrl)
			metrics := NewMocktripMetrics(ctrl)
			keys := NewMockidempotencyRepository(ctrl)
			in := validInput()
			key := uuid.New()
			in.IdempotencyKey = &key
			canonical := fmt.Sprintf(`{"driver_id":"%s","end_point":{"latitude":55.8,"longitude":37.7},"price":500,"start_point":{"latitude":55.75,"longitude":37.61},"user_id":"%s"}`, in.DriverID, in.UserID)
			hash := sha256.Sum256([]byte(canonical))
			existing := trip.Trip{ID: uuid.New(), Status: trip.StatusCompleted}
			runInTx(tx)
			var wantErr error
			switch name {
			case claimCase, completeErrorCase:
				keys.EXPECT().Claim(gomock.Any(), key, hash[:]).Return(uuid.Nil, true, nil)
				repo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
				repo.EXPECT().AddStatusChange(gomock.Any(), gomock.Any()).Return(nil)
				if name == completeErrorCase {
					wantErr = errDB
				} else {
					metrics.EXPECT().TripCreated(gomock.Any())
				}
				keys.EXPECT().Complete(gomock.Any(), key, gomock.Any()).Return(wantErr)
			case replayCase, readErrorCase:
				keys.EXPECT().Claim(gomock.Any(), key, hash[:]).Return(existing.ID, false, nil)
				if name == readErrorCase {
					wantErr = errDB
				}
				repo.EXPECT().GetByID(gomock.Any(), existing.ID).Return(existing, wantErr)
			case "conflict", claimErrorCase:
				wantErr = trip.ErrIdempotencyConflict
				if name == claimErrorCase {
					wantErr = errDB
				}
				keys.EXPECT().Claim(gomock.Any(), key, hash[:]).Return(uuid.Nil, false, wantErr)
			}
			got, err := create.New(repo, keys, tx, metrics, fixedAt).Execute(context.Background(), in)
			if !errors.Is(err, wantErr) {
				t.Fatalf("error = %v, want %v", err, wantErr)
			}
			if name == replayCase && (!got.Replayed || got.Trip != existing) {
				t.Fatalf("replay = %+v, want %+v", got, existing)
			}
			if name == claimCase && (got.Replayed || got.ID == uuid.Nil) {
				t.Fatalf("created = %+v", got)
			}
		})
	}
}
