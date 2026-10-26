//go:generate mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test
package create

import (
	"context"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
)

type (
	tripRepository interface {
		Create(ctx context.Context, t trip.Trip) error
		AddStatusChange(ctx context.Context, change trip.StatusChange) error
	}

	txManager interface {
		Do(ctx context.Context, fn func(ctx context.Context) error) error
	}
)
