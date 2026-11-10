package trip

import (
	"context"
	"errors"
	"fmt"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
	"github.com/fancyqqq/tripgo-avito/internal/infra/postgres/transaction"
)

const driverActiveIndex = "trips_driver_active_uniq"

func (r *Repository) Create(ctx context.Context, t trip.Trip) error {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := psql.
		Insert("trips").
		Columns(tripColumns...).
		Values(
			t.ID,
			t.UserID,
			t.DriverID,
			t.StartPoint.Latitude,
			t.StartPoint.Longitude,
			t.EndPoint.Latitude,
			t.EndPoint.Longitude,
			t.Price,
			string(t.Status),
			t.StartedAt,
			t.FinishedAt,
		).
		ToSql()
	if err != nil {
		return fmt.Errorf("build insert trip query: %w", err)
	}

	_, err = transaction.GetQuerier(ctx, r.pool).Exec(ctx, query, args...)

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == driverActiveIndex {
		return trip.ErrDriverBusy
	}
	if err != nil {
		return fmt.Errorf("insert trip: %w", err)
	}

	return nil
}

func (r *Repository) Update(ctx context.Context, t trip.Trip) error {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	query, args, err := psql.
		Update("trips").
		Set("status", string(t.Status)).
		Set("finished_at", t.FinishedAt).
		Set("updated_at", sq.Expr("now()")).
		Where(sq.Eq{"id": t.ID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build update trip query: %w", err)
	}

	tag, err := transaction.GetQuerier(ctx, r.pool).Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update trip: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return trip.ErrTripNotFound
	}

	return nil
}

func (r *Repository) AddStatusChange(ctx context.Context, change trip.StatusChange) error {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()

	var fromStatus *string
	if change.FromStatus != nil {
		s := string(*change.FromStatus)
		fromStatus = &s
	}

	query, args, err := psql.
		Insert("trip_status_history").
		Columns("trip_id", "from_status", "to_status", "reason").
		Values(change.TripID, fromStatus, string(change.ToStatus), change.Reason).
		ToSql()
	if err != nil {
		return fmt.Errorf("build insert status change query: %w", err)
	}

	if _, err := transaction.GetQuerier(ctx, r.pool).Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("insert status change: %w", err)
	}

	return nil
}
