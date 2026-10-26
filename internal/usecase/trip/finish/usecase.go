package finish

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
)

type UseCase struct {
	repo tripRepository
	tx   txManager
	now  func() time.Time
}

func New(repo tripRepository, tx txManager, now func() time.Time) *UseCase {
	return &UseCase{repo: repo, tx: tx, now: now}
}

func (u *UseCase) Execute(ctx context.Context, id uuid.UUID) (trip.Trip, error) {
	var finished trip.Trip

	err := u.tx.Do(ctx, func(ctx context.Context) error {
		t, err := u.repo.GetByIDForUpdate(ctx, id)
		if err != nil {
			return err
		}

		from := t.Status
		if err := t.Finish(u.now()); err != nil {
			return err
		}

		if err := u.repo.Update(ctx, t); err != nil {
			return err
		}

		if err := u.repo.AddStatusChange(ctx, trip.StatusChange{
			TripID:     t.ID,
			FromStatus: &from,
			ToStatus:   t.Status,
			Reason:     "trip completed",
		}); err != nil {
			return err
		}

		finished = t
		return nil
	})
	if err != nil {
		return trip.Trip{}, fmt.Errorf("finish trip: %w", err)
	}

	return finished, nil
}
