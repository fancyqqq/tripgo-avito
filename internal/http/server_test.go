package http_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
	"github.com/fancyqqq/tripgo-avito/internal/generated"
	httptransport "github.com/fancyqqq/tripgo-avito/internal/http"
	"github.com/fancyqqq/tripgo-avito/internal/http/health"
	"github.com/fancyqqq/tripgo-avito/internal/http/trip/create"
	"github.com/fancyqqq/tripgo-avito/internal/http/trip/finish"
	"github.com/fancyqqq/tripgo-avito/internal/http/trip/get"
	"github.com/fancyqqq/tripgo-avito/internal/model"
)

type createStub struct{}

func (createStub) Execute(_ context.Context, in model.CreateTrip) (trip.Trip, error) {
	return trip.Trip{ID: uuid.New(), UserID: in.UserID, DriverID: in.DriverID, Status: trip.StatusActive}, nil
}

type tripStub struct {
	status trip.Status
}

func (s tripStub) Execute(_ context.Context, id uuid.UUID) (trip.Trip, error) {
	return trip.Trip{ID: id, Status: s.status}, nil
}

type pingerStub struct {
	err error
}

func (p pingerStub) Ping(context.Context) error {
	return p.err
}

func newRouter(pingErr error) http.Handler {
	logger := slog.New(slog.DiscardHandler)
	server := httptransport.NewServer(
		create.New(createStub{}, logger),
		get.New(tripStub{status: trip.StatusActive}, logger),
		finish.New(tripStub{status: trip.StatusCompleted}, logger),
		health.New(pingerStub{err: pingErr}, time.Second, logger),
		logger,
	)
	return server.Routes()
}

func TestRoutes(t *testing.T) {
	t.Parallel()

	id := uuid.New().String()

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{name: "get trip", method: http.MethodGet, path: "/api/v1/trips/" + id, wantStatus: http.StatusOK},
		{name: "finish trip", method: http.MethodPost, path: "/api/v1/trips/" + id + "/finish", wantStatus: http.StatusOK},
		{name: "health", method: http.MethodGet, path: "/health", wantStatus: http.StatusOK},
		{name: "ready", method: http.MethodGet, path: "/ready", wantStatus: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			newRouter(nil).ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d, body: %s", rec.Code, tt.wantStatus, rec.Body)
			}
		})
	}
}

func TestInvalidTripIDReturnsBadRequest(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/v1/trips/not-a-uuid"},
		{method: http.MethodPost, path: "/api/v1/trips/not-a-uuid/finish"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			newRouter(nil).ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
				t.Fatalf("content-type = %q, want application/problem+json", ct)
			}
			var p generated.Problem
			if err := json.NewDecoder(rec.Body).Decode(&p); err != nil {
				t.Fatalf("decode problem: %v", err)
			}
			if p.Code != "invalid_request" {
				t.Fatalf("code = %q, want invalid_request", p.Code)
			}
		})
	}
}

func TestReady(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		pingErr    error
		wantStatus int
		wantBody   generated.HealthResponseStatus
	}{
		{name: "database is up", wantStatus: http.StatusOK, wantBody: generated.Ok},
		{name: "database is down", pingErr: errors.New("connection refused"), wantStatus: http.StatusServiceUnavailable, wantBody: generated.Unavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			newRouter(tt.pingErr).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			var body generated.HealthResponse
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body.Status != tt.wantBody {
				t.Fatalf("body status = %q, want %q", body.Status, tt.wantBody)
			}
		})
	}
}

func TestHealthDoesNotDependOnDatabase(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	newRouter(errors.New("connection refused")).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}
