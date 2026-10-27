//go:generate mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test
package finish

import (
	"context"

	"github.com/google/uuid"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
)

type (
	tripRepository interface {
		GetByIDForUpdate(ctx context.Context, id uuid.UUID) (trip.Trip, error)
		Update(ctx context.Context, t trip.Trip) error
		AddStatusChange(ctx context.Context, change trip.StatusChange) error
	}

	txManager interface {
		Do(ctx context.Context, fn func(ctx context.Context) error) error
	}

	tripMetrics interface {
		TripCompleted(ctx context.Context)
	}
)
