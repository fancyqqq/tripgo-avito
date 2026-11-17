//go:generate go tool mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test
package listpositions

import (
	"context"

	"github.com/google/uuid"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
)

type useCase interface {
	Execute(ctx context.Context, id uuid.UUID) ([]trip.Position, error)
}
