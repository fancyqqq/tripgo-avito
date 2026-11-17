package listpositions_test

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
	"github.com/fancyqqq/tripgo-avito/internal/generated"
	"github.com/fancyqqq/tripgo-avito/internal/http/trip/listpositions"
)

func execute(t *testing.T, points []trip.Position, usecaseErr error) *httptest.ResponseRecorder {
	t.Helper()
	id := uuid.New()
	uc := NewMockuseCase(gomock.NewController(t))
	uc.EXPECT().Execute(gomock.Any(), id).Return(points, usecaseErr)
	handler := listpositions.New(uc, slog.New(slog.DiscardHandler))
	rec := httptest.NewRecorder()
	handler.Handle(rec, httptest.NewRequest(http.MethodGet, "/api/v1/trips/"+id.String()+"/positions", nil), id)
	return rec
}

func TestHandleRoute(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	first := time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)
	points := []trip.Position{
		{ID: 42, TripID: id, Coordinates: trip.Point{Latitude: 59.93, Longitude: 30.33}, RecordedAt: first},
		{ID: 43, TripID: id, Coordinates: trip.Point{Latitude: 59.94, Longitude: 30.34}, RecordedAt: first.Add(time.Second)},
	}
	rec := execute(t, points, nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("response = %d %s", rec.Code, rec.Body)
	}
	var got generated.TripPositionsResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Positions) != len(points) {
		t.Fatalf("route length = %d, want %d", len(got.Positions), len(points))
	}
	for i, point := range got.Positions {
		want := points[i]
		if point.Id != want.ID || point.TripId != want.TripID || !point.RecordedAt.Equal(want.RecordedAt) || point.Coordinates.Latitude != want.Coordinates.Latitude || point.Coordinates.Longitude != want.Coordinates.Longitude {
			t.Fatalf("position %d = %+v, want %+v", i, point, want)
		}
	}
}

func TestHandleEmptyRouteIsJSONArray(t *testing.T) {
	t.Parallel()
	for _, points := range [][]trip.Position{nil, {}} {
		rec := execute(t, points, nil)
		if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"positions":[]}` {
			t.Fatalf("empty route = %d %s", rec.Code, rec.Body)
		}
	}
}

func TestHandleErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "missing trip", err: trip.ErrTripNotFound, status: http.StatusNotFound, code: "trip_not_found"},
		{name: "storage failure", err: errors.New("private SQL detail"), status: http.StatusInternalServerError, code: "internal_error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := execute(t, nil, tt.err)
			if rec.Code != tt.status || rec.Header().Get("Content-Type") != "application/problem+json" {
				t.Fatalf("response = %d %s", rec.Code, rec.Body)
			}
			if strings.Contains(rec.Body.String(), "private SQL detail") {
				t.Fatal("internal error leaked to client")
			}
			var problem generated.Problem
			if err := json.NewDecoder(rec.Body).Decode(&problem); err != nil {
				t.Fatal(err)
			}
			if problem.Code != tt.code || int(problem.Status) != rec.Code {
				t.Fatalf("problem = %+v", problem)
			}
		})
	}
}
