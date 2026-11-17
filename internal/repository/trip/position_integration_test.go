//go:build integration

package trip_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
	"github.com/fancyqqq/tripgo-avito/internal/infra/postgres/transaction"
	"github.com/fancyqqq/tripgo-avito/internal/model"
	positionrepository "github.com/fancyqqq/tripgo-avito/internal/repository/position"
	triprepository "github.com/fancyqqq/tripgo-avito/internal/repository/trip"
	"github.com/fancyqqq/tripgo-avito/internal/usecase/trip/createposition"
	"github.com/fancyqqq/tripgo-avito/internal/usecase/trip/finish"
)

var errPositionRollback = errors.New("reject transaction after position writes")

type positionEffects struct {
	saved     atomic.Int64
	cancelled atomic.Int64
}

func (e *positionEffects) PositionSaved(context.Context) {
	e.saved.Add(1)
}

func (e *positionEffects) Cancel(uuid.UUID) {
	e.cancelled.Add(1)
}

type rejectPositionTx struct {
	manager *transaction.Manager
}

func (tx rejectPositionTx) Do(ctx context.Context, fn func(context.Context) error) error {
	return tx.manager.Do(ctx, func(ctx context.Context) error {
		if err := fn(ctx); err != nil {
			return err
		}
		return errPositionRollback
	})
}

func positionState(t *testing.T, pool *pgxpool.Pool, repo *triprepository.Repository, id uuid.UUID) (trip.Trip, int) {
	t.Helper()
	stored, err := repo.GetByID(context.Background(), id)
	if err != nil {
		t.Fatalf("read trip: %v", err)
	}
	var count int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM trip_positions WHERE trip_id = $1", id).Scan(&count); err != nil {
		t.Fatalf("count positions: %v", err)
	}
	return stored, count
}

func TestSavePositionsKeepsLatestRecordedTime(t *testing.T) {
	pool := newPool(t)
	repo := triprepository.New(pool, queryTimeout)
	tr := newTrip(t, uuid.New())
	create(t, repo, pool, tr)
	latest := tr.StartedAt.Add(5 * time.Second)
	effects := &positionEffects{}
	uc := createposition.New(repo, positionrepository.New(pool, queryTimeout), transaction.New(pool), effects, effects,
		func() time.Time { return tr.StartedAt.Add(10 * time.Second) })
	for _, recordedAt := range []time.Time{latest, tr.StartedAt, latest} {
		input := model.CreatePosition{TripID: tr.ID, Coordinates: trip.Point{Latitude: 59.93, Longitude: 30.33}, RecordedAt: recordedAt}
		saved, err := uc.Execute(context.Background(), input)
		if err != nil {
			t.Fatalf("save position: %v", err)
		}
		if saved.ID <= 0 || saved.TripID != tr.ID || saved.Coordinates != input.Coordinates || !saved.RecordedAt.Equal(recordedAt) || saved.CreatedAt.IsZero() {
			t.Fatalf("saved position = %+v", saved)
		}
	}
	stored, count := positionState(t, pool, repo, tr.ID)
	if count != 3 || stored.LastPositionAt == nil || !stored.LastPositionAt.Equal(latest) {
		t.Fatalf("positions = %d, last_position_at = %v, want 3 and %v", count, stored.LastPositionAt, latest)
	}
	if effects.saved.Load() != 3 || effects.cancelled.Load() != 3 {
		t.Fatalf("saved = %d, cancelled = %d", effects.saved.Load(), effects.cancelled.Load())
	}
}

func TestSavePositionRollsBackBothWrites(t *testing.T) {
	pool := newPool(t)
	repo := triprepository.New(pool, queryTimeout)
	tr := newTrip(t, uuid.New())
	create(t, repo, pool, tr)
	tx := transaction.New(pool)
	positions := positionrepository.New(pool, queryTimeout)
	effects := &positionEffects{}
	fixedNow := func() time.Time { return tr.StartedAt.Add(10 * time.Second) }
	input := model.CreatePosition{TripID: tr.ID, RecordedAt: tr.StartedAt}
	uc := createposition.New(repo, positions, tx, effects, effects, fixedNow)
	if _, err := uc.Execute(context.Background(), input); err != nil {
		t.Fatalf("save initial position: %v", err)
	}
	input.RecordedAt = tr.StartedAt.Add(time.Second)
	failing := createposition.New(repo, positions, rejectPositionTx{manager: tx}, effects, effects, fixedNow)
	got, err := failing.Execute(context.Background(), input)
	if !errors.Is(err, errPositionRollback) || got != (trip.Position{}) {
		t.Fatalf("Execute = %+v, %v, want rollback error", got, err)
	}
	stored, count := positionState(t, pool, repo, tr.ID)
	if count != 1 || stored.LastPositionAt == nil || !stored.LastPositionAt.Equal(tr.StartedAt) {
		t.Fatalf("after rollback: positions = %d, last_position_at = %v", count, stored.LastPositionAt)
	}
	if effects.saved.Load() != 1 || effects.cancelled.Load() != 1 {
		t.Fatal("rollback produced post-commit effects")
	}
}

func TestSavePositionRejectsCompletedTrip(t *testing.T) {
	pool := newPool(t)
	repo := triprepository.New(pool, queryTimeout)
	tr := newTrip(t, uuid.New())
	create(t, repo, pool, tr)
	if err := tr.Finish(tr.StartedAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Update(context.Background(), tr); err != nil {
		t.Fatal(err)
	}
	effects := &positionEffects{}
	uc := createposition.New(repo, positionrepository.New(pool, queryTimeout), transaction.New(pool), effects, effects, now)
	_, err := uc.Execute(context.Background(), model.CreatePosition{TripID: tr.ID, RecordedAt: tr.StartedAt})
	if !errors.Is(err, trip.ErrTripCompleted) {
		t.Fatalf("error = %v, want trip completed", err)
	}
	stored, count := positionState(t, pool, repo, tr.ID)
	if count != 0 || stored.LastPositionAt != nil || effects.saved.Load() != 0 || effects.cancelled.Load() != 0 {
		t.Fatal("completed trip accepted a position")
	}
}

func TestSavePositionAndFinishConcurrent(t *testing.T) {
	pool := newPool(t)
	repo := triprepository.New(pool, queryTimeout)
	tr := newTrip(t, uuid.New())
	create(t, repo, pool, tr)
	tx := transaction.New(pool)
	effects := &positionEffects{}
	fixedNow := func() time.Time { return tr.StartedAt.Add(2 * time.Second) }
	saveUC := createposition.New(repo, positionrepository.New(pool, queryTimeout), tx, effects, nil, fixedNow)
	finishUC := finish.New(repo, tx, &completedCounter{}, fixedNow)
	start := make(chan struct{})
	var wg sync.WaitGroup
	var saveErr, finishErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, saveErr = saveUC.Execute(context.Background(), model.CreatePosition{TripID: tr.ID, RecordedAt: tr.StartedAt.Add(time.Second)})
	}()
	go func() {
		defer wg.Done()
		<-start
		_, finishErr = finishUC.Execute(context.Background(), tr.ID)
	}()
	close(start)
	wg.Wait()
	if finishErr != nil || (saveErr != nil && !errors.Is(saveErr, trip.ErrTripCompleted)) {
		t.Fatalf("finish error = %v, save error = %v", finishErr, saveErr)
	}
	stored, count := positionState(t, pool, repo, tr.ID)
	wantCount := 1
	if saveErr != nil {
		wantCount = 0
	}
	if stored.Status != trip.StatusCompleted || count != wantCount || effects.saved.Load() != int64(wantCount) {
		t.Fatalf("status = %s, positions = %d, saved = %d", stored.Status, count, effects.saved.Load())
	}
	if (stored.LastPositionAt == nil) != (wantCount == 0) {
		t.Fatalf("last_position_at = %v, positions = %d", stored.LastPositionAt, count)
	}
}
