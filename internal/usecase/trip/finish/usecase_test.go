package finish_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
	"github.com/fancyqqq/tripgo-avito/internal/usecase/trip/finish"
)

var (
	startedAt  = time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	finishedAt = startedAt.Add(30 * time.Minute)
	errDB      = errors.New("db is down")
	fixedAt    = func() time.Time { return finishedAt }
)

func activeTrip(id uuid.UUID) trip.Trip {
	return trip.Trip{
		ID:        id,
		UserID:    uuid.New(),
		DriverID:  uuid.New(),
		Price:     500,
		Status:    trip.StatusActive,
		StartedAt: startedAt,
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
		setup   func(id uuid.UUID, repo *MocktripRepository, tx *MocktxManager)
		wantErr error
	}{
		{
			name: "trip not found",
			setup: func(id uuid.UUID, repo *MocktripRepository, tx *MocktxManager) {
				runInTx(tx)
				repo.EXPECT().GetByIDForUpdate(gomock.Any(), id).Return(trip.Trip{}, trip.ErrTripNotFound)
			},
			wantErr: trip.ErrTripNotFound,
		},
		{
			name: "trip already completed",
			setup: func(id uuid.UUID, repo *MocktripRepository, tx *MocktxManager) {
				runInTx(tx)
				completed := activeTrip(id)
				completed.Status = trip.StatusCompleted
				completed.FinishedAt = &startedAt
				repo.EXPECT().GetByIDForUpdate(gomock.Any(), id).Return(completed, nil)
			},
			wantErr: trip.ErrTripCompleted,
		},
		{
			name: "update fails",
			setup: func(id uuid.UUID, repo *MocktripRepository, tx *MocktxManager) {
				runInTx(tx)
				repo.EXPECT().GetByIDForUpdate(gomock.Any(), id).Return(activeTrip(id), nil)
				repo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(errDB)
			},
			wantErr: errDB,
		},
		{
			name: "history write fails",
			setup: func(id uuid.UUID, repo *MocktripRepository, tx *MocktxManager) {
				runInTx(tx)
				repo.EXPECT().GetByIDForUpdate(gomock.Any(), id).Return(activeTrip(id), nil)
				repo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
				repo.EXPECT().AddStatusChange(gomock.Any(), gomock.Any()).Return(errDB)
			},
			wantErr: errDB,
		},
		{
			name: "transaction fails",
			setup: func(_ uuid.UUID, _ *MocktripRepository, tx *MocktxManager) {
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
			id := uuid.New()
			tt.setup(id, repo, tx)

			got, err := finish.New(repo, tx, fixedAt).Execute(context.Background(), id)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if got != (trip.Trip{}) {
				t.Fatalf("expected zero trip on error, got %+v", got)
			}
		})
	}
}

func TestExecuteFinishesActiveTrip(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	repo := NewMocktripRepository(ctrl)
	tx := NewMocktxManager(ctrl)
	id := uuid.New()
	stored := activeTrip(id)

	var updated trip.Trip
	var change trip.StatusChange

	runInTx(tx)
	gomock.InOrder(
		repo.EXPECT().GetByIDForUpdate(gomock.Any(), id).Return(stored, nil),
		repo.EXPECT().Update(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, tr trip.Trip) error {
				updated = tr
				return nil
			}),
		repo.EXPECT().AddStatusChange(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, c trip.StatusChange) error {
				change = c
				return nil
			}),
	)

	got, err := finish.New(repo, tx, fixedAt).Execute(context.Background(), id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.Status != trip.StatusCompleted {
		t.Fatalf("status = %q, want %q", got.Status, trip.StatusCompleted)
	}
	if got.FinishedAt == nil || !got.FinishedAt.Equal(finishedAt) {
		t.Fatalf("finished_at = %v, want %v", got.FinishedAt, finishedAt)
	}
	if got.ID != stored.ID || got.UserID != stored.UserID || got.DriverID != stored.DriverID || got.Price != stored.Price {
		t.Fatalf("trip fields changed: got %+v, stored %+v", got, stored)
	}
	if updated != got {
		t.Fatalf("updated trip = %+v, returned %+v", updated, got)
	}

	if change.TripID != id {
		t.Fatalf("history trip id = %v, want %v", change.TripID, id)
	}
	if change.FromStatus == nil || *change.FromStatus != trip.StatusActive {
		t.Fatalf("history from status = %v, want %q", change.FromStatus, trip.StatusActive)
	}
	if change.ToStatus != trip.StatusCompleted {
		t.Fatalf("history to status = %q, want %q", change.ToStatus, trip.StatusCompleted)
	}
	if change.Reason == "" {
		t.Fatal("history reason is empty")
	}
}
