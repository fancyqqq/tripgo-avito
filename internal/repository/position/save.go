package position

import (
	"context"
	"fmt"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
	"github.com/fancyqqq/tripgo-avito/internal/infra/postgres/transaction"
)

func (r *Repository) Create(ctx context.Context, position trip.Position) (trip.Position, error) {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := psql.Insert("trip_positions").
		Columns("trip_id", "latitude", "longitude", "recorded_at").
		Values(position.TripID, position.Coordinates.Latitude, position.Coordinates.Longitude, position.RecordedAt).
		Suffix("RETURNING id, trip_id, latitude, longitude, recorded_at, created_at").ToSql()
	if err != nil {
		return trip.Position{}, fmt.Errorf("build insert position query: %w", err)
	}

	var saved trip.Position
	err = transaction.GetQuerier(ctx, r.pool).QueryRow(ctx, query, args...).Scan(
		&saved.ID, &saved.TripID, &saved.Coordinates.Latitude, &saved.Coordinates.Longitude,
		&saved.RecordedAt, &saved.CreatedAt,
	)
	if err != nil {
		return trip.Position{}, fmt.Errorf("insert position: %w", err)
	}
	saved.RecordedAt = saved.RecordedAt.UTC()
	saved.CreatedAt = saved.CreatedAt.UTC()
	return saved, nil
}
