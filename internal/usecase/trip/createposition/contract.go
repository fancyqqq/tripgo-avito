//go:generate go tool mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test
package createposition

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
)

type (
	tripRepository interface {
		GetByIDForUpdate(ctx context.Context, id uuid.UUID) (trip.Trip, error)
		UpdateLastPositionAt(ctx context.Context, id uuid.UUID, recordedAt time.Time) error
	}
	positionRepository interface {
		Create(ctx context.Context, position trip.Position) (trip.Position, error)
	}
	txManager interface {
		Do(ctx context.Context, fn func(context.Context) error) error
	}
	positionMetrics interface {
		PositionSaved(ctx context.Context)
	}
	positionRequests interface {
		Cancel(id uuid.UUID)
	}
)
