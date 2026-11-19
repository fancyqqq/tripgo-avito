package createposition_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
	"github.com/fancyqqq/tripgo-avito/internal/model"
	"github.com/fancyqqq/tripgo-avito/internal/usecase/trip/createposition"
)

var errStorage = errors.New("storage failed")

type dependencies struct {
	trips     *MocktripRepository
	positions *MockpositionRepository
	tx        *MocktxManager
	metrics   *MockpositionMetrics
	requests  *MockpositionRequests
}

func newDependencies(t *testing.T) dependencies {
	t.Helper()
	ctrl := gomock.NewController(t)
	return dependencies{
		trips: NewMocktripRepository(ctrl), positions: NewMockpositionRepository(ctrl),
		tx: NewMocktxManager(ctrl), metrics: NewMockpositionMetrics(ctrl), requests: NewMockpositionRequests(ctrl),
	}
}

func inTransaction(tx *MocktxManager, commitErr error) {
	tx.EXPECT().Do(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
		if err := fn(ctx); err != nil {
			return err
		}
		return commitErr
	})
}

func TestExecuteCommitsBeforeSideEffects(t *testing.T) {
	t.Parallel()
	d := newDependencies(t)
	now := time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)
	in := model.CreatePosition{TripID: uuid.New(), Coordinates: trip.Point{Latitude: 59.93, Longitude: 30.33}, RecordedAt: now}
	position, err := trip.NewPosition(in.TripID, in.Coordinates, in.RecordedAt, now)
	if err != nil {
		t.Fatal(err)
	}
	saved := position
	saved.ID = 42
	saved.CreatedAt = now
	ctx := context.Background()
	txCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	committed := false
	d.tx.EXPECT().Do(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, fn func(context.Context) error) error {
		if err := fn(txCtx); err != nil {
			return err
		}
		committed = true
		return nil
	})
	gomock.InOrder(
		d.trips.EXPECT().GetByIDForUpdate(txCtx, in.TripID).Return(trip.Trip{ID: in.TripID, Status: trip.StatusActive}, nil),
		d.positions.EXPECT().Create(txCtx, position).Return(saved, nil),
		d.trips.EXPECT().UpdateLastPositionAt(txCtx, in.TripID, saved.RecordedAt).Return(nil),
		d.requests.EXPECT().Cancel(in.TripID).Do(func(uuid.UUID) {
			if !committed {
				t.Fatal("request cancelled before commit")
			}
		}),
		d.metrics.EXPECT().PositionSaved(ctx).Do(func(context.Context) {
			if !committed {
				t.Fatal("metric incremented before commit")
			}
		}),
	)
	got, err := createposition.New(d.trips, d.positions, d.tx, d.metrics, d.requests, func() time.Time { return now }).Execute(ctx, in)
	if err != nil || got != saved {
		t.Fatalf("Execute = %+v, %v, want %+v", got, err, saved)
	}
}

func TestExecuteErrorsHaveNoSideEffects(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		setup   func(dependencies, uuid.UUID)
		wantErr error
	}{
		{name: "transaction start", setup: func(d dependencies, _ uuid.UUID) {
			d.tx.EXPECT().Do(gomock.Any(), gomock.Any()).Return(errStorage)
		}, wantErr: errStorage},
		{name: "trip missing", setup: func(d dependencies, id uuid.UUID) {
			inTransaction(d.tx, nil)
			d.trips.EXPECT().GetByIDForUpdate(gomock.Any(), id).Return(trip.Trip{}, trip.ErrTripNotFound)
		}, wantErr: trip.ErrTripNotFound},
		{name: "trip completed", setup: func(d dependencies, id uuid.UUID) {
			inTransaction(d.tx, nil)
			d.trips.EXPECT().GetByIDForUpdate(gomock.Any(), id).Return(trip.Trip{Status: trip.StatusCompleted}, nil)
		}, wantErr: trip.ErrTripCompleted},
		{name: "insert fails", setup: func(d dependencies, id uuid.UUID) {
			inTransaction(d.tx, nil)
			d.trips.EXPECT().GetByIDForUpdate(gomock.Any(), id).Return(trip.Trip{Status: trip.StatusActive}, nil)
			d.positions.EXPECT().Create(gomock.Any(), gomock.Any()).Return(trip.Position{}, errStorage)
		}, wantErr: errStorage},
		{name: "time update fails", setup: func(d dependencies, id uuid.UUID) {
			inTransaction(d.tx, nil)
			d.trips.EXPECT().GetByIDForUpdate(gomock.Any(), id).Return(trip.Trip{Status: trip.StatusActive}, nil)
			d.positions.EXPECT().Create(gomock.Any(), gomock.Any()).Return(trip.Position{RecordedAt: now}, nil)
			d.trips.EXPECT().UpdateLastPositionAt(gomock.Any(), id, now).Return(errStorage)
		}, wantErr: errStorage},
		{name: "commit fails", setup: func(d dependencies, id uuid.UUID) {
			inTransaction(d.tx, errStorage)
			d.trips.EXPECT().GetByIDForUpdate(gomock.Any(), id).Return(trip.Trip{Status: trip.StatusActive}, nil)
			d.positions.EXPECT().Create(gomock.Any(), gomock.Any()).Return(trip.Position{RecordedAt: now}, nil)
			d.trips.EXPECT().UpdateLastPositionAt(gomock.Any(), id, now).Return(nil)
		}, wantErr: errStorage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := newDependencies(t)
			in := model.CreatePosition{TripID: uuid.New(), RecordedAt: now}
			tt.setup(d, in.TripID)
			got, err := createposition.New(d.trips, d.positions, d.tx, d.metrics, d.requests, func() time.Time { return now }).Execute(context.Background(), in)
			if !errors.Is(err, tt.wantErr) || got != (trip.Position{}) {
				t.Fatalf("Execute = %+v, %v, want %v", got, err, tt.wantErr)
			}
		})
	}
}

func TestExecuteRejectsInvalidInputBeforeTransaction(t *testing.T) {
	t.Parallel()
	d := newDependencies(t)
	now := time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)
	in := model.CreatePosition{TripID: uuid.New(), RecordedAt: now.Add(2 * time.Minute)}
	_, err := createposition.New(d.trips, d.positions, d.tx, d.metrics, d.requests, func() time.Time { return now }).Execute(context.Background(), in)
	var validation *trip.ValidationError
	if !errors.As(err, &validation) || validation.Field != "recorded_at" {
		t.Fatalf("error = %v, want recorded_at validation", err)
	}
}
