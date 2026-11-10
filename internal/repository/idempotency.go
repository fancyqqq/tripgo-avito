package repository

import (
	"bytes"
	"context"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/fancyqqq/tripgo-avito/internal/domain"
	"github.com/fancyqqq/tripgo-avito/internal/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const idempotencyTTL = 24 * time.Hour

type IdempotencyRepository struct {
	pool         *pgxpool.Pool
	queryTimeout time.Duration
}

func NewIdempotencyRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *IdempotencyRepository {
	return &IdempotencyRepository{pool: pool, queryTimeout: queryTimeout}
}

func (r *IdempotencyRepository) Claim(ctx context.Context, key uuid.UUID, requestHash []byte) (tripID uuid.UUID, claimed bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := psql.Insert("trip_idempotency").
		Columns("idempotency_key", "request_hash", "expires_at").
		Values(key, requestHash, sq.Expr("statement_timestamp() + make_interval(secs => ?)", idempotencyTTL.Seconds())).
		Suffix(`ON CONFLICT (idempotency_key) DO UPDATE
            SET request_hash = EXCLUDED.request_hash, trip_id = NULL, expires_at = EXCLUDED.expires_at
            WHERE trip_idempotency.expires_at <= statement_timestamp()`).
		ToSql()
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("build claim idempotency key query: %w", err)
	}
	q := postgres.GetQuerier(ctx, r.pool)
	tag, err := q.Exec(ctx, query, args...)
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("claim idempotency key: %w", err)
	}
	if tag.RowsAffected() == 1 {
		return uuid.Nil, true, nil
	}

	query, args, err = psql.Select("request_hash", "trip_id").
		From("trip_idempotency").Where(sq.Eq{"idempotency_key": key}).ToSql()
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("build get idempotency key query: %w", err)
	}
	var storedHash []byte
	if err := q.QueryRow(ctx, query, args...).Scan(&storedHash, &tripID); err != nil {
		return uuid.Nil, false, fmt.Errorf("get idempotency key: %w", err)
	}
	if !bytes.Equal(storedHash, requestHash) {
		return uuid.Nil, false, domain.ErrIdempotencyConflict
	}
	return tripID, false, nil
}

func (r *IdempotencyRepository) Complete(ctx context.Context, key, tripID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := psql.Update("trip_idempotency").Set("trip_id", tripID).
		Where(sq.Eq{"idempotency_key": key, "trip_id": nil}).ToSql()
	if err != nil {
		return fmt.Errorf("build complete idempotency key query: %w", err)
	}
	tag, err := postgres.GetQuerier(ctx, r.pool).Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("complete idempotency key: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("complete idempotency key: reservation not found")
	}
	return nil
}
