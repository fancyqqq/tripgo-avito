package trip

import (
	"time"

	"github.com/google/uuid"
)

type Position struct {
	ID          int64
	TripID      uuid.UUID
	Coordinates Point
	RecordedAt  time.Time
	CreatedAt   time.Time
}
