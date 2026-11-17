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

func NewPosition(tripID uuid.UUID, coordinates Point, recordedAt, now time.Time) (Position, error) {
	if tripID == uuid.Nil {
		return Position{}, &ValidationError{Field: "trip_id", Reason: "must be a non-zero UUID"}
	}
	if err := coordinates.validate("coordinates"); err != nil {
		return Position{}, err
	}
	if recordedAt.IsZero() {
		return Position{}, &ValidationError{Field: "recorded_at", Reason: "is required"}
	}
	if recordedAt.After(now.Add(time.Minute)) {
		return Position{}, &ValidationError{Field: "recorded_at", Reason: "must not be more than one minute in the future"}
	}
	return Position{TripID: tripID, Coordinates: coordinates, RecordedAt: recordedAt.UTC()}, nil
}

func (t Trip) CanRecordPosition() error {
	if t.Status != StatusActive {
		return ErrTripCompleted
	}
	return nil
}
