package create_test

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
	"github.com/fancyqqq/tripgo-avito/internal/http/trip/create"
	"github.com/fancyqqq/tripgo-avito/internal/model"
)

const path = "/api/v1/trips"

var (
	userID   = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	driverID = uuid.MustParse("22222222-2222-2222-2222-222222222222")
)

const validBody = `{
	"user_id": "11111111-1111-1111-1111-111111111111",
	"driver_id": "22222222-2222-2222-2222-222222222222",
	"start_point": {"latitude": 55.75, "longitude": 37.61},
	"end_point": {"latitude": 55.80, "longitude": 37.70},
	"price": 500
}`

func newHandler(t *testing.T) (*create.Handler, *MockuseCase) {
	t.Helper()
	uc := NewMockuseCase(gomock.NewController(t))
	return create.New(uc, slog.New(slog.DiscardHandler)), uc
}

func do(h *create.Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Handle(rec, req)
	return rec
}

func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder) generated.Problem {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("content-type = %q, want application/problem+json", ct)
	}
	var p generated.Problem
	if err := json.NewDecoder(rec.Body).Decode(&p); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	return p
}

func TestHandleCreated(t *testing.T) {
	t.Parallel()

	h, uc := newHandler(t)
	startedAt := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)

	wantInput := model.CreateTrip{
		UserID:   userID,
		DriverID: driverID,
		Start:    trip.Point{Latitude: 55.75, Longitude: 37.61},
		End:      trip.Point{Latitude: 55.80, Longitude: 37.70},
		Price:    500,
	}
	created := trip.Trip{
		ID:         uuid.New(),
		UserID:     userID,
		DriverID:   driverID,
		StartPoint: wantInput.Start,
		EndPoint:   wantInput.End,
		Price:      500,
		Status:     trip.StatusActive,
		StartedAt:  startedAt,
	}
	uc.EXPECT().Execute(gomock.Any(), wantInput).Return(created, nil)

	rec := do(h, validBody)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body)
	}
	if got, want := rec.Header().Get("Location"), path+"/"+created.ID.String(); got != want {
		t.Fatalf("location = %q, want %q", got, want)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q, want application/json", ct)
	}

	var body generated.Trip
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	want := generated.Trip{
		Id:         created.ID,
		UserId:     userID,
		DriverId:   driverID,
		StartPoint: generated.Coordinates{Latitude: 55.75, Longitude: 37.61},
		EndPoint:   generated.Coordinates{Latitude: 55.80, Longitude: 37.70},
		Price:      500,
		Status:     generated.Active,
		StartedAt:  startedAt,
	}
	if body != want {
		t.Fatalf("body = %+v, want %+v", body, want)
	}
}

func TestHandleInvalidBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       string
		wantDetail string
	}{
		{
			name:       "malformed json",
			body:       `{"user_id":`,
			wantDetail: "request body must be a JSON object",
		},
		{
			name:       "not an object",
			body:       `[1, 2, 3]`,
			wantDetail: "request body must be a JSON object",
		},
		{
			name:       "missing price",
			body: `{
				"user_id": "11111111-1111-1111-1111-111111111111",
				"driver_id": "22222222-2222-2222-2222-222222222222",
				"start_point": {"latitude": 55.75, "longitude": 37.61},
				"end_point": {"latitude": 55.80, "longitude": 37.70}
			}`,
			wantDetail: "price is required",
		},
		{
			name:       "null user id",
			body:       strings.Replace(validBody, `"11111111-1111-1111-1111-111111111111"`, `null`, 1),
			wantDetail: "user_id is required",
		},
		{
			name:       "missing nested latitude",
			body:       strings.Replace(validBody, `{"latitude": 55.75, "longitude": 37.61}`, `{"longitude": 37.61}`, 1),
			wantDetail: "start_point.latitude is required",
		},
		{
			name:       "point is not an object",
			body:       strings.Replace(validBody, `{"latitude": 55.80, "longitude": 37.70}`, `"here"`, 1),
			wantDetail: "end_point must be a JSON object",
		},
		{
			name: "unknown field",
			body: strings.Replace(validBody, `"price": 500`, `"price": 500, "color": "red"`, 1),
		},
		{
			name: "invalid uuid",
			body: strings.Replace(validBody, `"11111111-1111-1111-1111-111111111111"`, `"not-a-uuid"`, 1),
		},
		{
			name: "price is a string",
			body: strings.Replace(validBody, `"price": 500`, `"price": "500"`, 1),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h, _ := newHandler(t)
			rec := do(h, tt.body)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusBadRequest, rec.Body)
			}
			p := decodeProblem(t, rec)
			if p.Code != "invalid_request" {
				t.Fatalf("code = %q, want invalid_request", p.Code)
			}
			if p.Instance == nil || *p.Instance != path {
				t.Fatalf("instance = %v, want %q", p.Instance, path)
			}
			if tt.wantDetail != "" && (p.Detail == nil || *p.Detail != tt.wantDetail) {
				t.Fatalf("detail = %v, want %q", p.Detail, tt.wantDetail)
			}
		})
	}
}

func TestHandleUseCaseErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{
			name:       "validation error",
			err:        &trip.ValidationError{Field: "price", Reason: "must be >= 0"},
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_request",
		},
		{
			name:       "driver busy",
			err:        trip.ErrDriverBusy,
			wantStatus: http.StatusConflict,
			wantCode:   "driver_busy",
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

			h, uc := newHandler(t)
			uc.EXPECT().Execute(gomock.Any(), gomock.Any()).Return(trip.Trip{}, tt.err)

			rec := do(h, validBody)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if p := decodeProblem(t, rec); p.Code != tt.wantCode {
				t.Fatalf("code = %q, want %q", p.Code, tt.wantCode)
			}
			if rec.Header().Get("Location") != "" {
				t.Fatal("location must not be set on error")
			}
		})
	}
}
