package domain

import (
	"time"

	"github.com/google/uuid"
)

type TripStatus string

const (
	StatusActive    TripStatus = "active"
	StatusCompleted TripStatus = "completed"
)

type Point struct {
	Latitude  float64
	Longitude float64
}

type Trip struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	DriverID   uuid.UUID
	StartPoint Point
	EndPoint   Point
	Price      int64
	Status     TripStatus
	StartedAt  time.Time
	FinishedAt *time.Time
}

type StatusChange struct {
	TripID     uuid.UUID
	FromStatus *TripStatus
	ToStatus   TripStatus
	Reason     string
}
