package trip_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
)

func TestNew(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	validPoint := trip.Point{Latitude: 55.75, Longitude: 37.61}

	tests := []struct {
		name      string
		userID    uuid.UUID
		driverID  uuid.UUID
		start     trip.Point
		end       trip.Point
		price     int64
		wantField string
	}{
		{
			name:     "valid",
			userID:   uuid.New(),
			driverID: uuid.New(),
			start:    validPoint,
			end:      trip.Point{Latitude: 55.80, Longitude: 37.70},
			price:    100,
		},
		{
			name:     "zero price",
			userID:   uuid.New(),
			driverID: uuid.New(),
			start:    validPoint,
			end:      validPoint,
			price:    0,
		},
		{
			name:     "max boundaries",
			userID:   uuid.New(),
			driverID: uuid.New(),
			start:    trip.Point{Latitude: 90, Longitude: 180},
			end:      trip.Point{Latitude: 90, Longitude: 180},
			price:    100,
		},
		{
			name:     "min boundaries",
			userID:   uuid.New(),
			driverID: uuid.New(),
			start:    trip.Point{Latitude: -90, Longitude: -180},
			end:      trip.Point{Latitude: -90, Longitude: -180},
			price:    100,
		},
		{
			name:      "empty user id",
			userID:    uuid.Nil,
			driverID:  uuid.New(),
			start:     validPoint,
			end:       validPoint,
			price:     100,
			wantField: "user_id",
		},
		{
			name:      "empty driver id",
			userID:    uuid.New(),
			driverID:  uuid.Nil,
			start:     validPoint,
			end:       validPoint,
			price:     100,
			wantField: "driver_id",
		},
		{
			name:      "start latitude above max",
			userID:    uuid.New(),
			driverID:  uuid.New(),
			start:     trip.Point{Latitude: 90.1, Longitude: 0},
			end:       validPoint,
			price:     100,
			wantField: "start_point.latitude",
		},
		{
			name:      "start latitude below min",
			userID:    uuid.New(),
			driverID:  uuid.New(),
			start:     trip.Point{Latitude: -90.1, Longitude: 0},
			end:       validPoint,
			price:     100,
			wantField: "start_point.latitude",
		},
		{
			name:      "start longitude above max",
			userID:    uuid.New(),
			driverID:  uuid.New(),
			start:     trip.Point{Latitude: 0, Longitude: 180.1},
			end:       validPoint,
			price:     100,
			wantField: "start_point.longitude",
		},
		{
			name:      "start longitude below min",
			userID:    uuid.New(),
			driverID:  uuid.New(),
			start:     trip.Point{Latitude: 0, Longitude: -180.1},
			end:       validPoint,
			price:     100,
			wantField: "start_point.longitude",
		},
		{
			name:      "end latitude out of range",
			userID:    uuid.New(),
			driverID:  uuid.New(),
			start:     validPoint,
			end:       trip.Point{Latitude: 91, Longitude: 0},
			price:     100,
			wantField: "end_point.latitude",
		},
		{
			name:      "end longitude out of range",
			userID:    uuid.New(),
			driverID:  uuid.New(),
			start:     validPoint,
			end:       trip.Point{Latitude: 0, Longitude: -181},
			price:     100,
			wantField: "end_point.longitude",
		},
		{
			name:      "negative price",
			userID:    uuid.New(),
			driverID:  uuid.New(),
			start:     validPoint,
			end:       validPoint,
			price:     -1,
			wantField: "price",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			id := uuid.New()
			got, err := trip.New(id, tt.userID, tt.driverID, tt.start, tt.end, tt.price, now)

			if tt.wantField != "" {
				var vErr *trip.ValidationError
				if !errors.As(err, &vErr) {
					t.Fatalf("expected ValidationError, got %v", err)
				}
				if vErr.Field != tt.wantField {
					t.Fatalf("field = %q, want %q", vErr.Field, tt.wantField)
				}
				if got != (trip.Trip{}) {
					t.Fatalf("expected zero trip on error, got %+v", got)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			want := trip.Trip{
				ID:         id,
				UserID:     tt.userID,
				DriverID:   tt.driverID,
				StartPoint: tt.start,
				EndPoint:   tt.end,
				Price:      tt.price,
				Status:     trip.StatusActive,
				StartedAt:  now,
			}
			if got != want {
				t.Fatalf("trip = %+v, want %+v", got, want)
			}
		})
	}
}

func TestFinish(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(30 * time.Minute)
	earlierFinish := startedAt.Add(10 * time.Minute)

	tests := []struct {
		name           string
		trip           trip.Trip
		wantErr        error
		wantStatus     trip.Status
		wantFinishedAt *time.Time
	}{
		{
			name:           "active trip",
			trip:           trip.Trip{Status: trip.StatusActive, StartedAt: startedAt},
			wantStatus:     trip.StatusCompleted,
			wantFinishedAt: &finishedAt,
		},
		{
			name:           "completed trip",
			trip:           trip.Trip{Status: trip.StatusCompleted, StartedAt: startedAt, FinishedAt: &earlierFinish},
			wantErr:        trip.ErrTripCompleted,
			wantStatus:     trip.StatusCompleted,
			wantFinishedAt: &earlierFinish,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tr := tt.trip
			err := tr.Finish(finishedAt)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if tr.Status != tt.wantStatus {
				t.Fatalf("status = %q, want %q", tr.Status, tt.wantStatus)
			}
			if tr.FinishedAt == nil {
				t.Fatal("finished_at is nil")
			}
			if !tr.FinishedAt.Equal(*tt.wantFinishedAt) {
				t.Fatalf("finished_at = %v, want %v", *tr.FinishedAt, *tt.wantFinishedAt)
			}
		})
	}
}

func TestValidationErrorMessage(t *testing.T) {
	t.Parallel()

	err := &trip.ValidationError{Field: "price", Reason: "must be >= 0"}

	if got, want := err.Error(), "price must be >= 0"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}
