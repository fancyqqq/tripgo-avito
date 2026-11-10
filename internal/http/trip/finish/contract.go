//go:generate mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test
package finish

import (
	"context"

	"github.com/google/uuid"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
)

type useCase interface {
	Execute(ctx context.Context, id uuid.UUID) (trip.Trip, error)
}
