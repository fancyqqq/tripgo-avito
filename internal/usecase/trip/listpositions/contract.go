//go:generate go tool mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test
package listpositions

import (
	"context"

	"github.com/google/uuid"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
)

type (
	tripRepository interface {
		GetByID(ctx context.Context, id uuid.UUID) (trip.Trip, error)
	}
	positionRepository interface {
		ListByTripID(ctx context.Context, id uuid.UUID) ([]trip.Position, error)
	}
)
