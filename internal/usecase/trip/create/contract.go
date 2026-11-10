//go:generate mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test
package create

import (
	"context"

	"github.com/google/uuid"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
)

type (
	tripRepository interface {
		GetByID(ctx context.Context, id uuid.UUID) (trip.Trip, error)
		Create(ctx context.Context, t trip.Trip) error
		AddStatusChange(ctx context.Context, change trip.StatusChange) error
	}

	idempotencyRepository interface {
		Claim(ctx context.Context, key uuid.UUID, requestHash []byte) (uuid.UUID, bool, error)
		Complete(ctx context.Context, key, tripID uuid.UUID) error
	}

	txManager interface {
		Do(ctx context.Context, fn func(ctx context.Context) error) error
	}

	tripMetrics interface {
		TripCreated(ctx context.Context)
	}
)
