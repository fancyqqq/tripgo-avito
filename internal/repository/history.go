package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/fancyqqq/tripgo-avito/internal/domain"
	"github.com/fancyqqq/tripgo-avito/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

type HistoryRepository struct {
	pool         *pgxpool.Pool
	queryTimeout time.Duration
}

func NewHistoryRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *HistoryRepository {
	return &HistoryRepository{pool: pool, queryTimeout: queryTimeout}
}

func (r *HistoryRepository) Add(ctx context.Context, change domain.StatusChange) error {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := psql.
		Insert("trip_status_history").
		Columns("trip_id", "from_status", "to_status", "reason").
		Values(change.TripID, change.FromStatus, change.ToStatus, change.Reason).
		ToSql()
	if err != nil {
		return fmt.Errorf("build insert status change query: %w", err)
	}

	q := postgres.GetQuerier(ctx, r.pool)
	_, err = q.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("insert status change: %w", err)
	}

	return nil
}
