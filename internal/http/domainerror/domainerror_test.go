package domainerror_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
	"github.com/fancyqqq/tripgo-avito/internal/http/domainerror"
)

func TestProblem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
		wantType   string
		wantDetail string
	}{
		{
			name:       "validation error",
			err:        &trip.ValidationError{Field: "price", Reason: "must be >= 0"},
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_request",
			wantType:   "https://tripgo.example/problems/invalid-request",
			wantDetail: "price must be >= 0",
		},
		{
			name:       "wrapped validation error",
			err:        fmt.Errorf("create trip: %w", &trip.ValidationError{Field: "user_id", Reason: "must not be empty"}),
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_request",
			wantType:   "https://tripgo.example/problems/invalid-request",
			wantDetail: "user_id must not be empty",
		},
		{
			name:       "invalid request",
			err:        domainerror.InvalidRequest(errors.New("price is required")),
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_request",
			wantType:   "https://tripgo.example/problems/invalid-request",
			wantDetail: "price is required",
		},
		{
			name:       "trip not found",
			err:        fmt.Errorf("get trip: %w", trip.ErrTripNotFound),
			wantStatus: http.StatusNotFound,
			wantCode:   "trip_not_found",
			wantType:   "https://tripgo.example/problems/trip-not-found",
			wantDetail: "Trip was not found",
		},
		{
			name:       "trip completed",
			err:        fmt.Errorf("finish trip: %w", trip.ErrTripCompleted),
			wantStatus: http.StatusConflict,
			wantCode:   "trip_completed",
			wantType:   "https://tripgo.example/problems/trip-completed",
			wantDetail: "Operation is not allowed for a completed trip",
		},
		{
			name:       "driver busy",
			err:        fmt.Errorf("create trip: %w", trip.ErrDriverBusy),
			wantStatus: http.StatusConflict,
			wantCode:   "driver_busy",
			wantType:   "https://tripgo.example/problems/driver-busy",
			wantDetail: "Driver already has an active trip",
		},
		{
			name:       "unknown error",
			err:        errors.New("connection refused: password=secret"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   "internal_error",
			wantType:   "https://tripgo.example/problems/internal-error",
			wantDetail: "Internal server error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, problem := domainerror.Problem(tt.err)

			if status != tt.wantStatus {
				t.Fatalf("status = %d, want %d", status, tt.wantStatus)
			}
			if int(problem.Status) != tt.wantStatus {
				t.Fatalf("problem.status = %d, want %d", problem.Status, tt.wantStatus)
			}
			if problem.Code != tt.wantCode {
				t.Fatalf("code = %q, want %q", problem.Code, tt.wantCode)
			}
			if problem.Type != tt.wantType {
				t.Fatalf("type = %q, want %q", problem.Type, tt.wantType)
			}
			if problem.Title == "" {
				t.Fatal("title is empty")
			}
			if problem.Detail == nil || *problem.Detail != tt.wantDetail {
				t.Fatalf("detail = %v, want %q", problem.Detail, tt.wantDetail)
			}
		})
	}
}

func TestInvalidRequestUnwrap(t *testing.T) {
	t.Parallel()

	cause := errors.New("bad uuid")
	err := domainerror.InvalidRequest(cause)

	if !errors.Is(err, cause) {
		t.Fatal("InvalidRequest must keep the original error")
	}
	if err.Error() != cause.Error() {
		t.Fatalf("Error() = %q, want %q", err.Error(), cause.Error())
	}
}
