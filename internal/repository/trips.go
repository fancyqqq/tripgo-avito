package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/fancyqqq/tripgo-avito/internal/domain"
	"github.com/fancyqqq/tripgo-avito/internal/postgres"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var psql = sq.StatementBuilder.PlaceholderFormat(sq.Dollar)

type TripRepository struct {
	pool         *pgxpool.Pool
	queryTimeout time.Duration
}

func NewTripRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *TripRepository {
	return &TripRepository{pool: pool, queryTimeout: queryTimeout}
}

var tripColumns = []string{
	"id",
	"user_id",
	"driver_id",
	"start_latitude",
	"start_longitude",
	"end_latitude",
	"end_longitude",
	"price",
	"status",
	"started_at",
	"finished_at",
}

func scanTrip(row pgx.Row) (domain.Trip, error) {
	var trip domain.Trip
	var status string

	err := row.Scan(
		&trip.ID,
		&trip.UserID,
		&trip.DriverID,
		&trip.StartPoint.Latitude,
		&trip.StartPoint.Longitude,
		&trip.EndPoint.Latitude,
		&trip.EndPoint.Longitude,
		&trip.Price,
		&status,
		&trip.StartedAt,
		&trip.FinishedAt,
	)
	if err != nil {
		return domain.Trip{}, err
	}

	trip.Status = domain.TripStatus(status)
	return trip, nil
}

func (r *TripRepository) Create(ctx context.Context, trip domain.Trip) error {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := psql.
		Insert("trips").
		Columns(tripColumns...).
		Values(
			trip.ID,
			trip.UserID,
			trip.DriverID,
			trip.StartPoint.Latitude,
			trip.StartPoint.Longitude,
			trip.EndPoint.Latitude,
			trip.EndPoint.Longitude,
			trip.Price,
			trip.Status,
			trip.StartedAt,
			trip.FinishedAt,
		).
		ToSql()
	if err != nil {
		return fmt.Errorf("build insert trip query: %w", err)
	}

	q := postgres.GetQuerier(ctx, r.pool)
	_, err = q.Exec(ctx, query, args...)

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "trips_driver_active_uniq" {
		return domain.ErrDriverBusy
	}

	if err != nil {
		return fmt.Errorf("insert trip: %w", err)
	}

	return nil
}

func (r *TripRepository) GetByID(ctx context.Context, id uuid.UUID) (domain.Trip, error) {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := psql.
		Select(tripColumns...).
		From("trips").
		Where(sq.Eq{"id": id}).
		ToSql()
	if err != nil {
		return domain.Trip{}, fmt.Errorf("build get trip query: %w", err)
	}

	row := postgres.GetQuerier(ctx, r.pool).QueryRow(ctx, query, args...)
	trip, err := scanTrip(row)

	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Trip{}, domain.ErrTripNotFound
	}

	if err != nil {
		return domain.Trip{}, fmt.Errorf("get trip: %w", err)
	}

	return trip, nil
}

func (r *TripRepository) Finish(ctx context.Context, id uuid.UUID, finishedAt time.Time) (domain.Trip, error) {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := psql.
		Update("trips").
		Set("status", string(domain.StatusCompleted)).
		Set("finished_at", finishedAt).
		Set("updated_at", sq.Expr("now()")).
		Where(sq.Eq{"id": id, "status": string(domain.StatusActive)}).
		Suffix("RETURNING " + strings.Join(tripColumns, ", ")).
		ToSql()
	if err != nil {
		return domain.Trip{}, fmt.Errorf("build finish trip query: %w", err)
	}

	row := postgres.GetQuerier(ctx, r.pool).QueryRow(ctx, query, args...)
	trip, err := scanTrip(row)

	if errors.Is(err, pgx.ErrNoRows) {
		if _, getErr := r.GetByID(ctx, id); getErr != nil {
			return domain.Trip{}, getErr
		}
		return domain.Trip{}, domain.ErrTripCompleted
	}

	if err != nil {
		return domain.Trip{}, fmt.Errorf("finish trip: %w", err)
	}

	return trip, nil
}
