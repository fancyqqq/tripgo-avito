package model

import (
	"time"

	"github.com/google/uuid"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
)

type CreatePosition struct {
	TripID      uuid.UUID
	Coordinates trip.Point
	RecordedAt  time.Time
}

type StaleTrip struct {
	TripID         uuid.UUID
	DriverID       uuid.UUID
	LastActivityAt time.Time
}
