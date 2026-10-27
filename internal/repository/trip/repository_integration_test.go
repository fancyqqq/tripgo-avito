//go:build integration

package trip_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
	"github.com/fancyqqq/tripgo-avito/internal/infra/postgres/transaction"
	triprepository "github.com/fancyqqq/tripgo-avito/internal/repository/trip"
	finishusecase "github.com/fancyqqq/tripgo-avito/internal/usecase/trip/finish"
)

const queryTimeout = 5 * time.Second

func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Fatal("DATABASE_URL is not set, run tests via make test-integration")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("ping database: %v", err)
	}
	t.Cleanup(pool.Close)

	return pool
}

func now() time.Time {
	return time.Now().UTC().Truncate(time.Microsecond)
}

func newTrip(t *testing.T, driverID uuid.UUID) trip.Trip {
	t.Helper()

	tr, err := trip.New(
		uuid.New(),
		uuid.New(),
		driverID,
		trip.Point{Latitude: 55.75, Longitude: 37.61},
		trip.Point{Latitude: 55.80, Longitude: 37.70},
		500,
		now(),
	)
	if err != nil {
		t.Fatalf("new trip: %v", err)
	}
	return tr
}

func cleanup(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) {
	t.Helper()
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), "DELETE FROM trips WHERE id = $1", id); err != nil {
			t.Errorf("cleanup trip %s: %v", id, err)
		}
	})
}

func create(t *testing.T, repo *triprepository.Repository, pool *pgxpool.Pool, tr trip.Trip) {
	t.Helper()
	if err := repo.Create(context.Background(), tr); err != nil {
		t.Fatalf("create trip: %v", err)
	}
	cleanup(t, pool, tr.ID)
}

func assertTripEqual(t *testing.T, got, want trip.Trip) {
	t.Helper()

	if got.ID != want.ID || got.UserID != want.UserID || got.DriverID != want.DriverID {
		t.Fatalf("ids: got %+v, want %+v", got, want)
	}
	if got.StartPoint != want.StartPoint || got.EndPoint != want.EndPoint {
		t.Fatalf("points: got %+v / %+v, want %+v / %+v", got.StartPoint, got.EndPoint, want.StartPoint, want.EndPoint)
	}
	if got.Price != want.Price || got.Status != want.Status {
		t.Fatalf("price/status: got %d %q, want %d %q", got.Price, got.Status, want.Price, want.Status)
	}
	if !got.StartedAt.Equal(want.StartedAt) {
		t.Fatalf("started_at = %v, want %v", got.StartedAt, want.StartedAt)
	}
	switch {
	case got.FinishedAt == nil && want.FinishedAt == nil:
	case got.FinishedAt == nil || want.FinishedAt == nil:
		t.Fatalf("finished_at = %v, want %v", got.FinishedAt, want.FinishedAt)
	case !got.FinishedAt.Equal(*want.FinishedAt):
		t.Fatalf("finished_at = %v, want %v", *got.FinishedAt, *want.FinishedAt)
	}
}

func TestCreateAndGet(t *testing.T) {
	pool := newPool(t)
	repo := triprepository.New(pool, queryTimeout)
	tr := newTrip(t, uuid.New())

	create(t, repo, pool, tr)

	got, err := repo.GetByID(context.Background(), tr.ID)
	if err != nil {
		t.Fatalf("get trip: %v", err)
	}
	assertTripEqual(t, got, tr)
}

func TestGetNotFound(t *testing.T) {
	pool := newPool(t)
	repo := triprepository.New(pool, queryTimeout)

	if _, err := repo.GetByID(context.Background(), uuid.New()); !errors.Is(err, trip.ErrTripNotFound) {
		t.Fatalf("GetByID error = %v, want %v", err, trip.ErrTripNotFound)
	}
	if _, err := repo.GetByIDForUpdate(context.Background(), uuid.New()); !errors.Is(err, trip.ErrTripNotFound) {
		t.Fatalf("GetByIDForUpdate error = %v, want %v", err, trip.ErrTripNotFound)
	}
}

func TestCreateDriverBusy(t *testing.T) {
	pool := newPool(t)
	repo := triprepository.New(pool, queryTimeout)
	driverID := uuid.New()

	create(t, repo, pool, newTrip(t, driverID))

	second := newTrip(t, driverID)
	err := repo.Create(context.Background(), second)
	if err == nil {
		cleanup(t, pool, second.ID)
	}
	if !errors.Is(err, trip.ErrDriverBusy) {
		t.Fatalf("error = %v, want %v", err, trip.ErrDriverBusy)
	}
}

func TestDriverCanStartNewTripAfterFinish(t *testing.T) {
	pool := newPool(t)
	repo := triprepository.New(pool, queryTimeout)
	driverID := uuid.New()

	first := newTrip(t, driverID)
	create(t, repo, pool, first)

	if err := first.Finish(now()); err != nil {
		t.Fatalf("finish: %v", err)
	}
	if err := repo.Update(context.Background(), first); err != nil {
		t.Fatalf("update: %v", err)
	}

	create(t, repo, pool, newTrip(t, driverID))
}

func TestUpdate(t *testing.T) {
	pool := newPool(t)
	repo := triprepository.New(pool, queryTimeout)
	tr := newTrip(t, uuid.New())
	create(t, repo, pool, tr)

	if err := tr.Finish(now()); err != nil {
		t.Fatalf("finish: %v", err)
	}
	if err := repo.Update(context.Background(), tr); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, err := repo.GetByID(context.Background(), tr.ID)
	if err != nil {
		t.Fatalf("get trip: %v", err)
	}
	assertTripEqual(t, got, tr)
}

func TestUpdateNotFound(t *testing.T) {
	pool := newPool(t)
	repo := triprepository.New(pool, queryTimeout)
	tr := newTrip(t, uuid.New())

	if err := repo.Update(context.Background(), tr); !errors.Is(err, trip.ErrTripNotFound) {
		t.Fatalf("error = %v, want %v", err, trip.ErrTripNotFound)
	}
}

func TestAddStatusChange(t *testing.T) {
	pool := newPool(t)
	repo := triprepository.New(pool, queryTimeout)
	tr := newTrip(t, uuid.New())
	create(t, repo, pool, tr)

	active := trip.StatusActive
	changes := []trip.StatusChange{
		{TripID: tr.ID, ToStatus: trip.StatusActive, Reason: "trip created"},
		{TripID: tr.ID, FromStatus: &active, ToStatus: trip.StatusCompleted, Reason: "trip completed"},
	}
	for _, c := range changes {
		if err := repo.AddStatusChange(context.Background(), c); err != nil {
			t.Fatalf("add status change: %v", err)
		}
	}

	rows, err := pool.Query(context.Background(),
		"SELECT from_status, to_status, reason FROM trip_status_history WHERE trip_id = $1 ORDER BY id", tr.ID)
	if err != nil {
		t.Fatalf("query history: %v", err)
	}
	defer rows.Close()

	var got []trip.StatusChange
	for rows.Next() {
		var from *string
		var to, reason string
		if err := rows.Scan(&from, &to, &reason); err != nil {
			t.Fatalf("scan history: %v", err)
		}
		c := trip.StatusChange{TripID: tr.ID, ToStatus: trip.Status(to), Reason: reason}
		if from != nil {
			s := trip.Status(*from)
			c.FromStatus = &s
		}
		got = append(got, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	if len(got) != len(changes) {
		t.Fatalf("history length = %d, want %d", len(got), len(changes))
	}
	if got[0].FromStatus != nil || got[0].ToStatus != trip.StatusActive || got[0].Reason != "trip created" {
		t.Fatalf("first change = %+v", got[0])
	}
	if got[1].FromStatus == nil || *got[1].FromStatus != trip.StatusActive || got[1].ToStatus != trip.StatusCompleted {
		t.Fatalf("second change = %+v", got[1])
	}
}

func TestConcurrentFinishCompletesOnce(t *testing.T) {
	pool := newPool(t)
	repo := triprepository.New(pool, queryTimeout)
	tr := newTrip(t, uuid.New())
	create(t, repo, pool, tr)

	uc := finishusecase.New(repo, transaction.New(pool), now)

	const workers = 5
	errs := make([]error, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup

	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, errs[i] = uc.Execute(context.Background(), tr.ID)
		}()
	}
	close(start)
	wg.Wait()

	var succeeded int
	for _, err := range errs {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, trip.ErrTripCompleted):
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if succeeded != 1 {
		t.Fatalf("succeeded = %d, want 1", succeeded)
	}

	var completedChanges int
	err := pool.QueryRow(context.Background(),
		"SELECT count(*) FROM trip_status_history WHERE trip_id = $1 AND to_status = 'completed'", tr.ID,
	).Scan(&completedChanges)
	if err != nil {
		t.Fatalf("count history: %v", err)
	}
	if completedChanges != 1 {
		t.Fatalf("completed history rows = %d, want 1", completedChanges)
	}
}
