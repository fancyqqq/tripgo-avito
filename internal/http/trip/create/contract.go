//go:generate mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test
package create

import (
	"context"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
	"github.com/fancyqqq/tripgo-avito/internal/model"
)

type useCase interface {
	Execute(ctx context.Context, in model.CreateTrip) (trip.Trip, error)
}
