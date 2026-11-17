//go:build integration

package trip_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
	"github.com/fancyqqq/tripgo-avito/internal/infra/postgres/transaction"
	"github.com/fancyqqq/tripgo-avito/internal/model"
	triprepository "github.com/fancyqqq/tripgo-avito/internal/repository/trip"
)

var errStaleFixtureRollback = errors.New("rollback stale trip fixtures")

type staleFixture struct {
	name      string
	trip      trip.Trip
	wantAt60  bool
	wantAt120 bool
}

func staleFixtures(at time.Time) []staleFixture {
	older := at.Add(-2 * time.Minute)
	fresh := at.Add(-10 * time.Second)
	boundary := at.Add(-time.Minute)
	justStale := boundary.Add(-time.Microsecond)
	justFresh := boundary.Add(time.Microsecond)
	tied := at.Add(-90 * time.Second)
	lowID := uuid.New()
	lowID[15] = 1
	highID := lowID
	highID[15] = 2
	fixtures := []staleFixture{
		{name: "old trip without positions", trip: trip.Trip{StartedAt: at.Add(-3 * time.Minute)}, wantAt60: true, wantAt120: true},
		{name: "fresh trip without positions", trip: trip.Trip{StartedAt: at.Add(-20 * time.Second)}},
		{name: "stale position", trip: trip.Trip{LastPositionAt: &older}, wantAt60: true},
		{name: "fresh position on old trip", trip: trip.Trip{LastPositionAt: &fresh}},
		{name: "completed stale trip", trip: trip.Trip{Status: trip.StatusCompleted, LastPositionAt: &older}},
		{name: "completed trip without positions", trip: trip.Trip{Status: trip.StatusCompleted}},
		{name: "position exactly at boundary", trip: trip.Trip{LastPositionAt: &boundary}},
		{name: "position one microsecond too old", trip: trip.Trip{LastPositionAt: &justStale}, wantAt60: true},
		{name: "position one microsecond fresh", trip: trip.Trip{LastPositionAt: &justFresh}},
		{name: "start exactly at boundary", trip: trip.Trip{StartedAt: boundary}},
		{name: "equal time larger id inserted first", trip: trip.Trip{ID: highID, LastPositionAt: &tied}, wantAt60: true},
		{name: "equal time smaller id inserted last", trip: trip.Trip{ID: lowID, LastPositionAt: &tied}, wantAt60: true},
	}
	for i := range fixtures {
		t := &fixtures[i].trip
		if t.ID == uuid.Nil {
			t.ID = uuid.New()
		}
		t.UserID = uuid.New()
		t.DriverID = uuid.New()
		if t.StartedAt.IsZero() {
			t.StartedAt = at.Add(-time.Hour)
		}
		if t.Status == "" {
			t.Status = trip.StatusActive
		}
		if t.Status == trip.StatusCompleted {
			finished := at.Add(-5 * time.Second)
			t.FinishedAt = &finished
		}
	}
	return fixtures
}

func checkStaleSelection(candidates []model.StaleTrip, fixtures []staleFixture, threshold time.Duration) error {
	found := make(map[uuid.UUID]model.StaleTrip, len(candidates))
	for i, candidate := range candidates {
		found[candidate.TripID] = candidate
		if candidate.LastActivityAt.Location() != time.UTC {
			return fmt.Errorf("activity time is not UTC: %v", candidate.LastActivityAt)
		}
		if i == 0 {
			continue
		}
		previous := candidates[i-1]
		if previous.LastActivityAt.After(candidate.LastActivityAt) {
			return fmt.Errorf("stale trips are not ordered by activity time")
		}
		if previous.LastActivityAt.Equal(candidate.LastActivityAt) && bytes.Compare(previous.TripID[:], candidate.TripID[:]) >= 0 {
			return fmt.Errorf("stale trips with equal activity are not ordered by id")
		}
	}
	for _, fixture := range fixtures {
		candidate, exists := found[fixture.trip.ID]
		want := fixture.wantAt60
		if threshold == 2*time.Minute {
			want = fixture.wantAt120
		}
		if exists != want {
			return fmt.Errorf("%s at threshold %s: present=%t, want %t", fixture.name, threshold, exists, want)
		}
		if !exists {
			continue
		}
		activity := fixture.trip.StartedAt
		if fixture.trip.LastPositionAt != nil {
			activity = *fixture.trip.LastPositionAt
		}
		if candidate.DriverID != fixture.trip.DriverID || !candidate.LastActivityAt.Equal(activity) {
			return fmt.Errorf("%s: incorrect driver or activity time", fixture.name)
		}
	}
	return nil
}

func TestListStaleFiltersAndOrdersTrips(t *testing.T) {
	pool := newPool(t)
	repo := triprepository.New(pool, queryTimeout)
	err := transaction.New(pool).Do(context.Background(), func(ctx context.Context) error {
		var at time.Time
		if err := transaction.GetQuerier(ctx, pool).QueryRow(ctx, "SELECT now()").Scan(&at); err != nil {
			return err
		}
		fixtures := staleFixtures(at.UTC())
		for _, fixture := range fixtures {
			if err := repo.Create(ctx, fixture.trip); err != nil {
				return fmt.Errorf("insert %s: %w", fixture.name, err)
			}
		}
		for _, threshold := range []time.Duration{time.Minute, 2 * time.Minute} {
			candidates, err := repo.ListStale(ctx, threshold)
			if err != nil {
				return err
			}
			if err := checkStaleSelection(candidates, fixtures, threshold); err != nil {
				return err
			}
		}
		return errStaleFixtureRollback
	})
	if !errors.Is(err, errStaleFixtureRollback) {
		t.Fatalf("stale selection: %v", err)
	}
}

func TestListStaleHonorsCancelledContext(t *testing.T) {
	pool := newPool(t)
	repo := triprepository.New(pool, queryTimeout)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := repo.ListStale(ctx, time.Minute)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancelled", err)
	}
}
