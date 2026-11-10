package finish_test

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
	"github.com/fancyqqq/tripgo-avito/internal/http/trip/finish"
)

func finishPath(id uuid.UUID) string {
	return "/api/v1/trips/" + id.String() + "/finish"
}

func TestHandleFinished(t *testing.T) {
	t.Parallel()

	uc := NewMockuseCase(gomock.NewController(t))
	h := finish.New(uc, slog.New(slog.DiscardHandler))

	startedAt := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(30 * time.Minute)
	finished := trip.Trip{
		ID:         uuid.New(),
		UserID:     uuid.New(),
		DriverID:   uuid.New(),
		Price:      500,
		Status:     trip.StatusCompleted,
		StartedAt:  startedAt,
		FinishedAt: &finishedAt,
	}
	uc.EXPECT().Execute(gomock.Any(), finished.ID).Return(finished, nil)

	rec := httptest.NewRecorder()
	h.Handle(rec, httptest.NewRequest(http.MethodPost, finishPath(finished.ID), nil), finished.ID)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body)
	}

	var body generated.Trip
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Id != finished.ID {
		t.Fatalf("id = %v, want %v", body.Id, finished.ID)
	}
	if body.Status != generated.Completed {
		t.Fatalf("status = %q, want %q", body.Status, generated.Completed)
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
			name:       "already completed",
			err:        trip.ErrTripCompleted,
			wantStatus: http.StatusConflict,
			wantCode:   "trip_completed",
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
			h := finish.New(uc, slog.New(slog.DiscardHandler))
			id := uuid.New()
			uc.EXPECT().Execute(gomock.Any(), id).Return(trip.Trip{}, tt.err)

			rec := httptest.NewRecorder()
			h.Handle(rec, httptest.NewRequest(http.MethodPost, finishPath(id), nil), id)

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
		})
	}
}
