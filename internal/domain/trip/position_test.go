package trip_test

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
)

const (
	latitudeField  = "coordinates.latitude"
	longitudeField = "coordinates.longitude"
)

func TestNewPosition(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)
	id := uuid.New()
	tests := []struct {
		name      string
		id        uuid.UUID
		point     trip.Point
		at        time.Time
		wantField string
	}{
		{name: "zero coordinates", id: id, at: now},
		{name: "upper boundaries", id: id, point: trip.Point{Latitude: 90, Longitude: 180}, at: now},
		{name: "lower boundaries", id: id, point: trip.Point{Latitude: -90, Longitude: -180}, at: now},
		{name: "old point", id: id, at: now.Add(-24 * time.Hour)},
		{name: "future boundary", id: id, at: now.Add(time.Minute)},
		{name: "timezone", id: id, at: now.In(time.FixedZone("UTC+5", 5*60*60))},
		{name: "empty trip", at: now, wantField: "trip_id"},
		{name: "missing time", id: id, wantField: "recorded_at"},
		{name: "future exceeds boundary", id: id, at: now.Add(time.Minute + time.Nanosecond), wantField: "recorded_at"},
		{name: "latitude above maximum", id: id, point: trip.Point{Latitude: 90.1}, at: now, wantField: latitudeField},
		{name: "latitude below minimum", id: id, point: trip.Point{Latitude: -90.1}, at: now, wantField: latitudeField},
		{name: "longitude above maximum", id: id, point: trip.Point{Longitude: 180.1}, at: now, wantField: longitudeField},
		{name: "longitude below minimum", id: id, point: trip.Point{Longitude: -180.1}, at: now, wantField: longitudeField},
		{name: "NaN latitude", id: id, point: trip.Point{Latitude: math.NaN()}, at: now, wantField: latitudeField},
		{name: "NaN longitude", id: id, point: trip.Point{Longitude: math.NaN()}, at: now, wantField: longitudeField},
		{name: "infinite latitude", id: id, point: trip.Point{Latitude: math.Inf(1)}, at: now, wantField: latitudeField},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := trip.NewPosition(tt.id, tt.point, tt.at, now)
			if tt.wantField != "" {
				var validation *trip.ValidationError
				if !errors.As(err, &validation) || validation.Field != tt.wantField {
					t.Fatalf("error = %v, want validation for %s", err, tt.wantField)
				}
				if got != (trip.Position{}) {
					t.Fatalf("position on error = %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewPosition: %v", err)
			}
			if got.TripID != id || got.Coordinates != tt.point || !got.RecordedAt.Equal(tt.at) || got.RecordedAt.Location() != time.UTC {
				t.Fatalf("position = %+v", got)
			}
		})
	}
}

func TestCanRecordPosition(t *testing.T) {
	t.Parallel()
	for _, status := range []trip.Status{trip.StatusActive, trip.StatusCompleted} {
		t.Run(string(status), func(t *testing.T) {
			t.Parallel()
			err := (trip.Trip{Status: status}).CanRecordPosition()
			if status == trip.StatusActive && err != nil {
				t.Fatalf("active trip: %v", err)
			}
			if status == trip.StatusCompleted && !errors.Is(err, trip.ErrTripCompleted) {
				t.Fatalf("completed trip: %v", err)
			}
		})
	}
}
