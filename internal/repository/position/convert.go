package position

import (
	"github.com/jackc/pgx/v5"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
)

func scanPosition(row pgx.Row) (trip.Position, error) {
	var position trip.Position
	if err := row.Scan(
		&position.ID, &position.TripID, &position.Coordinates.Latitude, &position.Coordinates.Longitude,
		&position.RecordedAt, &position.CreatedAt,
	); err != nil {
		return trip.Position{}, err
	}
	position.RecordedAt = position.RecordedAt.UTC()
	position.CreatedAt = position.CreatedAt.UTC()
	return position, nil
}
