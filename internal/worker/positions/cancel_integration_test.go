//go:build integration

package positions_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric/noop"
	"go.uber.org/mock/gomock"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
	"github.com/fancyqqq/tripgo-avito/internal/infra/postgres/transaction"
	"github.com/fancyqqq/tripgo-avito/internal/infra/telemetry"
	"github.com/fancyqqq/tripgo-avito/internal/model"
	positionrepository "github.com/fancyqqq/tripgo-avito/internal/repository/position"
	triprepository "github.com/fancyqqq/tripgo-avito/internal/repository/trip"
	"github.com/fancyqqq/tripgo-avito/internal/usecase/trip/createposition"
	"github.com/fancyqqq/tripgo-avito/internal/worker/positions"
)

var errRollback = errors.New("reject position transaction")

type positionTx interface {
	Do(ctx context.Context, fn func(context.Context) error) error
}

type rejectedTx struct {
	manager *transaction.Manager
}

func (tx rejectedTx) Do(ctx context.Context, fn func(context.Context) error) error {
	return tx.manager.Do(ctx, func(ctx context.Context) error {
		if err := fn(ctx); err != nil {
			return err
		}
		return errRollback
	})
}

func newDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Fatal("DATABASE_URL is not set, run via make test-integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("create database pool: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping database: %v", err)
	}
	return pool
}

func TestPositionTransactionControlsQueuedRequest(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		name := "commit cancels request"
		if rollback {
			name = "rollback preserves request"
		}
		t.Run(name, func(t *testing.T) {
			database := newDatabase(t)
			repo := triprepository.New(database, 5*time.Second)
			now := time.Now().UTC().Truncate(time.Microsecond)
			tr, err := trip.New(uuid.New(), uuid.New(), uuid.New(), trip.Point{}, trip.Point{}, 0, now)
			if err != nil {
				t.Fatal(err)
			}
			if err := repo.Create(context.Background(), tr); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := database.Exec(context.Background(), "DELETE FROM trips WHERE id = $1", tr.ID); err != nil {
					t.Errorf("cleanup trip: %v", err)
				}
			})
			f := newFixture(t, poolConfig(1, 1), nil)
			enqueue(t, f.pool, model.StaleTrip{TripID: tr.ID, DriverID: tr.DriverID, LastActivityAt: tr.StartedAt}, positions.Queued)
			var tx positionTx = transaction.New(database)
			if rollback {
				tx = rejectedTx{manager: transaction.New(database)}
			}
			metrics, err := telemetry.NewTripMetrics(noop.NewMeterProvider().Meter("positions-test"))
			if err != nil {
				t.Fatal(err)
			}
			uc := createposition.New(repo, positionrepository.New(database, 5*time.Second), tx, metrics, f.pool, func() time.Time { return now })
			_, err = uc.Execute(context.Background(), model.CreatePosition{TripID: tr.ID, RecordedAt: now})
			if rollback {
				if !errors.Is(err, errRollback) {
					t.Fatalf("Execute: %v, want rollback", err)
				}
				f.gateway.EXPECT().RequestPosition(gomock.Any(), tr.ID, tr.DriverID).Return(nil)
				f.metrics.EXPECT().RequestFinished(gomock.Any(), success, gomock.Any())
			} else if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			assertPositionState(t, database, repo, tr.ID, rollback, now)
			wantPending := 0
			if rollback {
				wantPending = 1
			}
			if state := f.pool.Snapshot(); state.Pending != wantPending {
				t.Fatalf("pending = %d, want %d", state.Pending, wantPending)
			}
			start(t, f.pool, context.Background())
			shutdown(t, f.pool)
		})
	}
}

func assertPositionState(t *testing.T, database *pgxpool.Pool, repo *triprepository.Repository, id uuid.UUID, rollback bool, recordedAt time.Time) {
	t.Helper()
	stored, err := repo.GetByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.QueryRow(context.Background(), "SELECT count(*) FROM trip_positions WHERE trip_id = $1", id).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if rollback {
		if count != 0 || stored.LastPositionAt != nil {
			t.Fatalf("rollback: positions = %d, last_position_at = %v", count, stored.LastPositionAt)
		}
		return
	}
	if count != 1 || stored.LastPositionAt == nil || !stored.LastPositionAt.Equal(recordedAt) {
		t.Fatalf("commit: positions = %d, last_position_at = %v", count, stored.LastPositionAt)
	}
}
