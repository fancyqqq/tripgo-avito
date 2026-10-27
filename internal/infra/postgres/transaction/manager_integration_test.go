//go:build integration

package transaction_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/fancyqqq/tripgo-avito/internal/infra/postgres/transaction"
)

var errBoom = errors.New("boom")

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

func newTripID(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()

	id := uuid.New()
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), "DELETE FROM trips WHERE id = $1", id); err != nil {
			t.Errorf("cleanup trip %s: %v", id, err)
		}
	})
	return id
}

func insertTrip(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) error {
	_, err := transaction.GetQuerier(ctx, pool).Exec(ctx, `
		INSERT INTO trips (id, user_id, driver_id, start_latitude, start_longitude,
			end_latitude, end_longitude, price, status, started_at)
		VALUES ($1, $2, $3, 55.75, 37.61, 55.80, 37.70, 500, 'active', now())`,
		id, uuid.New(), uuid.New())
	return err
}

func tripExists(ctx context.Context, t *testing.T, q transaction.Querier, id uuid.UUID) bool {
	t.Helper()

	var exists bool
	if err := q.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM trips WHERE id = $1)", id).Scan(&exists); err != nil {
		t.Fatalf("check trip: %v", err)
	}
	return exists
}

func TestDoCommits(t *testing.T) {
	pool := newPool(t)
	id := newTripID(t, pool)

	err := transaction.New(pool).Do(context.Background(), func(ctx context.Context) error {
		return insertTrip(ctx, pool, id)
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	if !tripExists(context.Background(), t, pool, id) {
		t.Fatal("trip must be committed")
	}
}

func TestDoRollsBackOnError(t *testing.T) {
	pool := newPool(t)
	id := newTripID(t, pool)

	err := transaction.New(pool).Do(context.Background(), func(ctx context.Context) error {
		if err := insertTrip(ctx, pool, id); err != nil {
			return err
		}
		return errBoom
	})
	if !errors.Is(err, errBoom) {
		t.Fatalf("Do error = %v, want %v", err, errBoom)
	}

	if tripExists(context.Background(), t, pool, id) {
		t.Fatal("trip must be rolled back")
	}
}

func TestDoRollsBackOnPanic(t *testing.T) {
	pool := newPool(t)
	id := newTripID(t, pool)

	recovered := func() (p any) {
		defer func() { p = recover() }()
		_ = transaction.New(pool).Do(context.Background(), func(ctx context.Context) error {
			if err := insertTrip(ctx, pool, id); err != nil {
				return err
			}
			panic(errBoom)
		})
		return nil
	}()

	if recovered != errBoom {
		t.Fatalf("recovered = %v, want %v", recovered, errBoom)
	}
	if tripExists(context.Background(), t, pool, id) {
		t.Fatal("trip must be rolled back after panic")
	}
}

func TestNestedDoJoinsOuterTransaction(t *testing.T) {
	pool := newPool(t)
	id := newTripID(t, pool)
	manager := transaction.New(pool)

	err := manager.Do(context.Background(), func(ctx context.Context) error {
		if err := manager.Do(ctx, func(ctx context.Context) error {
			return insertTrip(ctx, pool, id)
		}); err != nil {
			return err
		}
		return errBoom
	})
	if !errors.Is(err, errBoom) {
		t.Fatalf("Do error = %v, want %v", err, errBoom)
	}

	if tripExists(context.Background(), t, pool, id) {
		t.Fatal("inner Do must not commit on its own")
	}
}

func TestUncommittedChangesAreInvisibleOutside(t *testing.T) {
	pool := newPool(t)
	id := newTripID(t, pool)

	err := transaction.New(pool).Do(context.Background(), func(ctx context.Context) error {
		if err := insertTrip(ctx, pool, id); err != nil {
			return err
		}
		if !tripExists(ctx, t, transaction.GetQuerier(ctx, pool), id) {
			t.Error("trip must be visible inside the transaction")
		}
		if tripExists(context.Background(), t, pool, id) {
			t.Error("trip must not be visible outside before commit")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
}

func TestGetQuerierWithoutTransactionUsesPool(t *testing.T) {
	pool := newPool(t)
	id := newTripID(t, pool)

	if err := insertTrip(context.Background(), pool, id); err != nil {
		t.Fatalf("insert: %v", err)
	}

	if !tripExists(context.Background(), t, pool, id) {
		t.Fatal("trip must be saved immediately without a transaction")
	}
}
