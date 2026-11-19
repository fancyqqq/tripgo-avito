//go:generate go tool mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test
package createposition

import (
	"context"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
	"github.com/fancyqqq/tripgo-avito/internal/model"
)

type useCase interface {
	Execute(ctx context.Context, in model.CreatePosition) (trip.Position, error)
}
