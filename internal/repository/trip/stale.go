package trip

import (
	"context"
	"fmt"
	"time"

	"github.com/fancyqqq/tripgo-avito/internal/infra/postgres/transaction"
	"github.com/fancyqqq/tripgo-avito/internal/model"
)

func (r *Repository) ListStale(ctx context.Context, staleAfter time.Duration) ([]model.StaleTrip, error) {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := psql.
		Select("id", "driver_id", "COALESCE(last_position_at, started_at) AS last_activity_at").
		From("trips").
		Where("status = 'active'").
		Where("COALESCE(last_position_at, started_at) < now() - (?::double precision * interval '1 second')", staleAfter.Seconds()).
		OrderBy("COALESCE(last_position_at, started_at) ASC", "id ASC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build stale trips query: %w", err)
	}
	rows, err := transaction.GetQuerier(ctx, r.pool).Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list stale trips: %w", err)
	}
	defer rows.Close()

	trips := make([]model.StaleTrip, 0)
	for rows.Next() {
		var candidate model.StaleTrip
		if err := rows.Scan(&candidate.TripID, &candidate.DriverID, &candidate.LastActivityAt); err != nil {
			return nil, fmt.Errorf("scan stale trip: %w", err)
		}
		candidate.LastActivityAt = candidate.LastActivityAt.UTC()
		trips = append(trips, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read stale trips: %w", err)
	}
	return trips, nil
}
