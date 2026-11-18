//go:generate go tool mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test
package positions

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/fancyqqq/tripgo-avito/internal/model"
)

type (
	pushGateway interface {
		RequestPosition(ctx context.Context, tripID, driverID uuid.UUID) error
	}
	requestMetrics interface {
		RequestDropped(ctx context.Context)
		RequestFinished(ctx context.Context, result string, duration time.Duration)
	}
	staleTrips interface {
		ListStale(ctx context.Context, staleAfter time.Duration) ([]model.StaleTrip, error)
	}
	positionQueue interface {
		Enqueue(ctx context.Context, trip model.StaleTrip) (EnqueueResult, error)
	}
	scanMetrics interface {
		StaleChecked(ctx context.Context, result string)
	}
	Ticker interface {
		C() <-chan time.Time
		Stop()
	}
)
