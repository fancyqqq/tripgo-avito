//go:build integration

package trip_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
	positionrepository "github.com/fancyqqq/tripgo-avito/internal/repository/position"
	triprepository "github.com/fancyqqq/tripgo-avito/internal/repository/trip"
	"github.com/fancyqqq/tripgo-avito/internal/usecase/trip/listpositions"
)

func TestListPositionsOrdersAndFiltersRoute(t *testing.T) {
	pool := newPool(t)
	trips := triprepository.New(pool, queryTimeout)
	positions := positionrepository.New(pool, queryTimeout)
	ctx := context.Background()
	tr := newTrip(t, uuid.New())
	other := newTrip(t, uuid.New())
	create(t, trips, pool, tr)
	create(t, trips, pool, other)
	uc := listpositions.New(trips, positions)

	empty, err := uc.Execute(ctx, tr.ID)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty route = %+v, %v", empty, err)
	}
	offsets := []int{3, 1, 1, 2}
	inserted := make([]trip.Position, 0, len(offsets))
	for _, seconds := range offsets {
		point := trip.Position{
			TripID: tr.ID, Coordinates: trip.Point{Latitude: 59.93, Longitude: 30.33},
			RecordedAt: tr.StartedAt.Add(time.Duration(seconds) * time.Second),
		}
		saved, err := positions.Create(ctx, point)
		if err != nil {
			t.Fatalf("insert route point: %v", err)
		}
		inserted = append(inserted, saved)
	}
	if _, err := positions.Create(ctx, trip.Position{TripID: other.ID, RecordedAt: tr.StartedAt}); err != nil {
		t.Fatalf("insert other route: %v", err)
	}
	want := []trip.Position{inserted[1], inserted[2], inserted[3], inserted[0]}
	got, err := uc.Execute(ctx, tr.ID)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("route = %+v, %v, want %+v", got, err, want)
	}

	if err := tr.Finish(tr.StartedAt.Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := trips.Update(ctx, tr); err != nil {
		t.Fatal(err)
	}
	got, err = uc.Execute(ctx, tr.ID)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("completed route = %+v, %v, want %+v", got, err, want)
	}
	got, err = uc.Execute(ctx, uuid.New())
	if !errors.Is(err, trip.ErrTripNotFound) || got != nil {
		t.Fatalf("missing trip route = %+v, %v", got, err)
	}
}
