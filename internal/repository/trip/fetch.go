package trip

import (
	"context"
	"errors"
	"fmt"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
	"github.com/fancyqqq/tripgo-avito/internal/infra/postgres/transaction"
)

func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (trip.Trip, error) {
	return r.fetch(ctx, id, false)
}

func (r *Repository) GetByIDForUpdate(ctx context.Context, id uuid.UUID) (trip.Trip, error) {
	return r.fetch(ctx, id, true)
}

func (r *Repository) fetch(ctx context.Context, id uuid.UUID, forUpdate bool) (trip.Trip, error) {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	builder := psql.
		Select(tripColumns...).
		From("trips").
		Where(sq.Eq{"id": id})
	if forUpdate {
		builder = builder.Suffix("FOR UPDATE")
	}

	query, args, err := builder.ToSql()
	if err != nil {
		return trip.Trip{}, fmt.Errorf("build get trip query: %w", err)
	}

	row := transaction.GetQuerier(ctx, r.pool).QueryRow(ctx, query, args...)
	t, err := scanTrip(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return trip.Trip{}, trip.ErrTripNotFound
	}
	if err != nil {
		return trip.Trip{}, fmt.Errorf("get trip: %w", err)
	}

	return t, nil
}
