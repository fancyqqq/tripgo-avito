package createposition_test

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
	"github.com/fancyqqq/tripgo-avito/internal/http/trip/createposition"
	"github.com/fancyqqq/tripgo-avito/internal/model"
)

const validBody = `{"coordinates":{"latitude":0,"longitude":0},"recorded_at":"2026-11-17T10:00:00Z"}`

func execute(t *testing.T, body string, setup func(*MockuseCase, uuid.UUID)) *httptest.ResponseRecorder {
	t.Helper()
	id := uuid.New()
	uc := NewMockuseCase(gomock.NewController(t))
	if setup != nil {
		setup(uc, id)
	}
	h := createposition.New(uc, slog.New(slog.DiscardHandler))
	rec := httptest.NewRecorder()
	h.Handle(rec, httptest.NewRequest(http.MethodPost, "/api/v1/trips/"+id.String()+"/positions", strings.NewReader(body)), id)
	return rec
}

func TestHandleCreated(t *testing.T) {
	t.Parallel()
	recordedAt := time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)
	var want trip.Position
	rec := execute(t, validBody, func(uc *MockuseCase, id uuid.UUID) {
		in := model.CreatePosition{TripID: id, RecordedAt: recordedAt}
		want = trip.Position{ID: 42, TripID: id, RecordedAt: recordedAt}
		uc.EXPECT().Execute(gomock.Any(), in).Return(want, nil)
	})
	if rec.Code != http.StatusCreated || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("response = %d %s", rec.Code, rec.Body)
	}
	var got generated.TripPosition
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Id != want.ID || got.TripId != want.TripID || !got.RecordedAt.Equal(want.RecordedAt) || got.Coordinates.Latitude != 0 || got.Coordinates.Longitude != 0 {
		t.Fatalf("position = %+v, want %+v", got, want)
	}
}

func TestHandleRejectsInvalidJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
	}{
		{name: "malformed", body: `{"coordinates":`},
		{name: "null body", body: `null`},
		{name: "array body", body: `[]`},
		{name: "missing coordinates", body: `{"recorded_at":"2026-11-17T10:00:00Z"}`},
		{name: "missing time", body: `{"coordinates":{"latitude":0,"longitude":0}}`},
		{name: "null coordinates", body: strings.Replace(validBody, `{"latitude":0,"longitude":0}`, `null`, 1)},
		{name: "null time", body: strings.Replace(validBody, `"2026-11-17T10:00:00Z"`, `null`, 1)},
		{name: "missing latitude", body: strings.Replace(validBody, `"latitude":0,`, ``, 1)},
		{name: "null longitude", body: strings.Replace(validBody, `"longitude":0`, `"longitude":null`, 1)},
		{name: "unknown top field", body: strings.Replace(validBody, `{"coordinates":`, `{"unexpected":1,"coordinates":`, 1)},
		{name: "unknown coordinate field", body: strings.Replace(validBody, `"latitude":0`, `"latitude":0,"altitude":1`, 1)},
		{name: "wrong field case", body: strings.Replace(validBody, `"latitude":0`, `"latitude":0,"Latitude":1`, 1)},
		{name: "bad time", body: strings.Replace(validBody, `2026-11-17T10:00:00Z`, `yesterday`, 1)},
		{name: "wrong coordinate type", body: strings.Replace(validBody, `"latitude":0`, `"latitude":"zero"`, 1)},
		{name: "trailing JSON", body: validBody + `{}`},
		{name: "oversize", body: strings.Repeat(" ", (1<<20)+1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := execute(t, tt.body, nil)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body: %s", rec.Code, rec.Body)
			}
			assertProblem(t, rec, "invalid_request")
		})
	}
}

func TestHandleDomainErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "validation", err: &trip.ValidationError{Field: "coordinates.latitude", Reason: "out of range"}, status: http.StatusBadRequest, code: "invalid_request"},
		{name: "missing", err: trip.ErrTripNotFound, status: http.StatusNotFound, code: "trip_not_found"},
		{name: "completed", err: trip.ErrTripCompleted, status: http.StatusConflict, code: "trip_completed"},
		{name: "internal", err: errors.New("private database detail"), status: http.StatusInternalServerError, code: "internal_error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := execute(t, validBody, func(uc *MockuseCase, _ uuid.UUID) {
				uc.EXPECT().Execute(gomock.Any(), gomock.Any()).Return(trip.Position{}, tt.err)
			})
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d", rec.Code, tt.status)
			}
			if strings.Contains(rec.Body.String(), "private database detail") {
				t.Fatal("internal error leaked to client")
			}
			assertProblem(t, rec, tt.code)
		})
	}
}

func assertProblem(t *testing.T, rec *httptest.ResponseRecorder, code string) {
	t.Helper()
	if rec.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("content-type = %s", rec.Header().Get("Content-Type"))
	}
	var problem generated.Problem
	if err := json.NewDecoder(rec.Body).Decode(&problem); err != nil {
		t.Fatal(err)
	}
	if problem.Code != code || int(problem.Status) != rec.Code {
		t.Fatalf("problem = %+v", problem)
	}
}
