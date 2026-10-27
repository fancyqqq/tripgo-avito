package create

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
	"github.com/fancyqqq/tripgo-avito/internal/model"
)

type UseCase struct {
	repo    tripRepository
	tx      txManager
	metrics tripMetrics
	now     func() time.Time
}

func New(repo tripRepository, tx txManager, metrics tripMetrics, now func() time.Time) *UseCase {
	return &UseCase{repo: repo, tx: tx, metrics: metrics, now: now}
}

func (u *UseCase) Execute(ctx context.Context, in model.CreateTrip) (trip.Trip, error) {
	t, err := trip.New(uuid.New(), in.UserID, in.DriverID, in.Start, in.End, in.Price, u.now())
	if err != nil {
		return trip.Trip{}, err
	}

	err = u.tx.Do(ctx, func(ctx context.Context) error {
		if err := u.repo.Create(ctx, t); err != nil {
			return err
		}
		return u.repo.AddStatusChange(ctx, trip.StatusChange{
			TripID:   t.ID,
			ToStatus: trip.StatusActive,
			Reason:   "trip created",
		})
	})
	if err != nil {
		return trip.Trip{}, fmt.Errorf("create trip: %w", err)
	}

	u.metrics.TripCreated(ctx)
	return t, nil
}
