//go:generate go tool mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test
package positions

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type (
	pushGateway interface {
		RequestPosition(ctx context.Context, tripID, driverID uuid.UUID) error
	}
	requestMetrics interface {
		RequestDropped(ctx context.Context)
		RequestFinished(ctx context.Context, result string, duration time.Duration)
	}
)
