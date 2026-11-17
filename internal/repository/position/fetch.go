package position

import (
	"context"
	"fmt"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
	"github.com/fancyqqq/tripgo-avito/internal/infra/postgres/transaction"
)

func (r *Repository) ListByTripID(ctx context.Context, id uuid.UUID) ([]trip.Position, error) {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := psql.Select("id", "trip_id", "latitude", "longitude", "recorded_at", "created_at").
		From("trip_positions").Where(sq.Eq{"trip_id": id}).OrderBy("recorded_at ASC", "id ASC").ToSql()
	if err != nil {
		return nil, fmt.Errorf("build list positions query: %w", err)
	}
	rows, err := transaction.GetQuerier(ctx, r.pool).Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list positions: %w", err)
	}
	defer rows.Close()

	positions := make([]trip.Position, 0)
	for rows.Next() {
		position, err := scanPosition(rows)
		if err != nil {
			return nil, fmt.Errorf("scan position: %w", err)
		}
		positions = append(positions, position)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read positions: %w", err)
	}
	return positions, nil
}
