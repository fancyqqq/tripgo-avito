package create

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
	"github.com/fancyqqq/tripgo-avito/internal/model"
)

type UseCase struct {
	idempotency idempotencyRepository
	repo        tripRepository
	tx          txManager
	metrics     tripMetrics
	now         func() time.Time
}

func New(repo tripRepository, idempotency idempotencyRepository, tx txManager, metrics tripMetrics, now func() time.Time) *UseCase {
	return &UseCase{repo: repo, idempotency: idempotency, tx: tx, metrics: metrics, now: now}
}

func (u *UseCase) Execute(ctx context.Context, in model.CreateTrip) (model.CreateTripResult, error) {
	t, err := trip.New(uuid.New(), in.UserID, in.DriverID, in.Start, in.End, in.Price, u.now())
	if err != nil {
		return model.CreateTripResult{}, err
	}

	// Keep the canonical representation compatible with keys stored before the refactor.
	canonicalBody, err := json.Marshal(map[string]any{
		"driver_id":   in.DriverID,
		"end_point":   map[string]float64{"latitude": in.End.Latitude, "longitude": in.End.Longitude},
		"price":       in.Price,
		"start_point": map[string]float64{"latitude": in.Start.Latitude, "longitude": in.Start.Longitude},
		"user_id":     in.UserID,
	})
	if err != nil {
		return model.CreateTripResult{}, fmt.Errorf("marshal create trip request: %w", err)
	}
	requestHash := sha256.Sum256(canonicalBody)
	replayed := false
	err = u.tx.Do(ctx, func(ctx context.Context) error {
		if in.IdempotencyKey != nil {
			tripID, claimed, err := u.idempotency.Claim(ctx, *in.IdempotencyKey, requestHash[:])
			if err != nil {
				return err
			}
			if !claimed {
				t, err = u.repo.GetByID(ctx, tripID)
				replayed = true
				return err
			}
		}
		if err := u.repo.Create(ctx, t); err != nil {
			return err
		}
		if err := u.repo.AddStatusChange(ctx, trip.StatusChange{
			TripID:   t.ID,
			ToStatus: trip.StatusActive,
			Reason:   "trip created",
		}); err != nil {
			return err
		}
		if in.IdempotencyKey != nil {
			return u.idempotency.Complete(ctx, *in.IdempotencyKey, t.ID)
		}
		return nil
	})
	if err != nil {
		return model.CreateTripResult{}, fmt.Errorf("create trip: %w", err)
	}

	if !replayed {
		u.metrics.TripCreated(ctx)
	}
	return model.CreateTripResult{Trip: t, Replayed: replayed}, nil
}
