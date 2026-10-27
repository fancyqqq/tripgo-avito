package get_test

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
	"github.com/fancyqqq/tripgo-avito/internal/generated"
	"github.com/fancyqqq/tripgo-avito/internal/http/trip/get"
)

func TestHandleFound(t *testing.T) {
	t.Parallel()

	uc := NewMockuseCase(gomock.NewController(t))
	h := get.New(uc, slog.New(slog.DiscardHandler))

	startedAt := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(30 * time.Minute)
	stored := trip.Trip{
		ID:         uuid.New(),
		UserID:     uuid.New(),
		DriverID:   uuid.New(),
		StartPoint: trip.Point{Latitude: 55.75, Longitude: 37.61},
		EndPoint:   trip.Point{Latitude: 55.80, Longitude: 37.70},
		Price:      500,
		Status:     trip.StatusCompleted,
		StartedAt:  startedAt,
		FinishedAt: &finishedAt,
	}
	uc.EXPECT().Execute(gomock.Any(), stored.ID).Return(stored, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/trips/"+stored.ID.String(), nil)
	rec := httptest.NewRecorder()
	h.Handle(rec, req, stored.ID)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q, want application/json", ct)
	}

	var body generated.Trip
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Id != stored.ID || body.UserId != stored.UserID || body.DriverId != stored.DriverID {
		t.Fatalf("ids mismatch: %+v", body)
	}
	if body.StartPoint != (generated.Coordinates{Latitude: 55.75, Longitude: 37.61}) {
		t.Fatalf("start_point = %+v", body.StartPoint)
	}
	if body.EndPoint != (generated.Coordinates{Latitude: 55.80, Longitude: 37.70}) {
		t.Fatalf("end_point = %+v", body.EndPoint)
	}
	if body.Price != 500 || body.Status != generated.Completed {
		t.Fatalf("price/status mismatch: %+v", body)
	}
	if !body.StartedAt.Equal(startedAt) {
		t.Fatalf("started_at = %v, want %v", body.StartedAt, startedAt)
	}
	if body.FinishedAt == nil || !body.FinishedAt.Equal(finishedAt) {
		t.Fatalf("finished_at = %v, want %v", body.FinishedAt, finishedAt)
	}
}

func TestHandleErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{
			name:       "not found",
			err:        trip.ErrTripNotFound,
			wantStatus: http.StatusNotFound,
			wantCode:   "trip_not_found",
		},
		{
			name:       "internal error",
			err:        errors.New("connection reset"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   "internal_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			uc := NewMockuseCase(gomock.NewController(t))
			h := get.New(uc, slog.New(slog.DiscardHandler))
			id := uuid.New()
			uc.EXPECT().Execute(gomock.Any(), id).Return(trip.Trip{}, tt.err)

			path := "/api/v1/trips/" + id.String()
			rec := httptest.NewRecorder()
			h.Handle(rec, httptest.NewRequest(http.MethodGet, path, nil), id)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
				t.Fatalf("content-type = %q, want application/problem+json", ct)
			}
			var p generated.Problem
			if err := json.NewDecoder(rec.Body).Decode(&p); err != nil {
				t.Fatalf("decode problem: %v", err)
			}
			if p.Code != tt.wantCode {
				t.Fatalf("code = %q, want %q", p.Code, tt.wantCode)
			}
			if p.Instance == nil || *p.Instance != path {
				t.Fatalf("instance = %v, want %q", p.Instance, path)
			}
		})
	}
}
