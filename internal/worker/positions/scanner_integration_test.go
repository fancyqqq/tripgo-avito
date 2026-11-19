//go:build integration

package positions_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace/noop"
	"go.uber.org/mock/gomock"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
	triprepository "github.com/fancyqqq/tripgo-avito/internal/repository/trip"
	"github.com/fancyqqq/tripgo-avito/internal/worker/positions"
)

func isolatedTripRepository(t *testing.T) *triprepository.Repository {
	t.Helper()
	database := newDatabase(t)
	ctx := context.Background()
	schema := "scanner_" + uuid.New().String()
	name := pgx.Identifier{schema}.Sanitize()
	if _, err := database.Exec(ctx, "CREATE SCHEMA "+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := database.Exec(ctx, "DROP SCHEMA "+name+" CASCADE"); err != nil {
			t.Errorf("drop test schema: %v", err)
		}
	})
	if _, err := database.Exec(ctx, "CREATE TABLE "+name+".trips (LIKE trips INCLUDING ALL)"); err != nil {
		t.Fatal(err)
	}
	cfg := database.Config().Copy()
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	isolated, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(isolated.Close)
	return triprepository.New(isolated, 5*time.Second)
}

func TestScannerSelectsStaleTripsFromPostgresAndDispatchesInOrder(t *testing.T) {
	repo := isolatedTripRepository(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	oldPosition := now.Add(-time.Hour)
	cases := []struct {
		startedAt      time.Time
		lastPositionAt *time.Time
		completed      bool
	}{
		{startedAt: now.Add(-2 * time.Hour)},
		{startedAt: now.Add(-3 * time.Hour), lastPositionAt: &oldPosition},
		{startedAt: now},
		{startedAt: now.Add(-3 * time.Hour), lastPositionAt: &now},
		{startedAt: now.Add(-3 * time.Hour), completed: true},
	}
	trips := make([]trip.Trip, 0, len(cases))
	for _, tc := range cases {
		tr, err := trip.New(uuid.New(), uuid.New(), uuid.New(), trip.Point{}, trip.Point{}, 0, tc.startedAt)
		if err != nil {
			t.Fatal(err)
		}
		tr.LastPositionAt = tc.lastPositionAt
		if tc.completed {
			if err := tr.Finish(now); err != nil {
				t.Fatal(err)
			}
		}
		if err := repo.Create(context.Background(), tr); err != nil {
			t.Fatal(err)
		}
		trips = append(trips, tr)
	}
	pool := newFixture(t, poolConfig(1, 5), nil)
	ctrl := gomock.NewController(t)
	metrics, ticker := NewMockscanMetrics(ctrl), NewMockTicker(ctrl)
	ticks := make(chan time.Time, 1)
	scanner, err := positions.NewScanner(scannerConfig(), repo, pool.pool, metrics, noop.NewTracerProvider().Tracer("integration"), slog.New(slog.DiscardHandler), func(time.Duration) positions.Ticker { return ticker })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(scanner.Close)
	finished := make(chan struct{}, 2)
	gomock.InOrder(
		pool.gateway.EXPECT().RequestPosition(gomock.Any(), trips[0].ID, trips[0].DriverID).Return(nil),
		pool.gateway.EXPECT().RequestPosition(gomock.Any(), trips[1].ID, trips[1].DriverID).Return(nil),
	)
	pool.metrics.EXPECT().RequestFinished(gomock.Any(), success, gomock.Any()).Times(2).Do(func(context.Context, string, time.Duration) { finished <- struct{}{} })
	metrics.EXPECT().StaleChecked(gomock.Any(), string(positions.Queued)).Times(2)
	start(t, pool.pool, context.Background())
	startScanner(t, scannerFixture{scanner: scanner, ticker: ticker, ticks: ticks}, context.Background())
	tick(t, ticks)
	for range 2 {
		waitFor(t, finished)
	}
	stopScanner(t, scanner)
	shutdown(t, pool.pool)
}
