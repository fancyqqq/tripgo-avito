package createposition

import (
	"context"
	"fmt"
	"time"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
	"github.com/fancyqqq/tripgo-avito/internal/model"
)

type UseCase struct {
	trips     tripRepository
	positions positionRepository
	tx        txManager
	metrics   positionMetrics
	requests  positionRequests
	now       func() time.Time
}

func New(trips tripRepository, positions positionRepository, tx txManager, metrics positionMetrics, requests positionRequests, now func() time.Time) *UseCase {
	return &UseCase{trips: trips, positions: positions, tx: tx, metrics: metrics, requests: requests, now: now}
}

func (u *UseCase) Execute(ctx context.Context, in model.CreatePosition) (trip.Position, error) {
	position, err := trip.NewPosition(in.TripID, in.Coordinates, in.RecordedAt, u.now())
	if err != nil {
		return trip.Position{}, fmt.Errorf("validate position: %w", err)
	}

	var saved trip.Position
	err = u.tx.Do(ctx, func(ctx context.Context) error {
		t, err := u.trips.GetByIDForUpdate(ctx, in.TripID)
		if err != nil {
			return err
		}
		if err := t.CanRecordPosition(); err != nil {
			return err
		}
		saved, err = u.positions.Create(ctx, position)
		if err != nil {
			return err
		}
		return u.trips.UpdateLastPositionAt(ctx, in.TripID, saved.RecordedAt)
	})
	if err != nil {
		return trip.Position{}, fmt.Errorf("create position: %w", err)
	}
	if u.requests != nil {
		u.requests.Cancel(in.TripID)
	}
	u.metrics.PositionSaved(ctx)
	return saved, nil
}
