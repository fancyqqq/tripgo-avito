package domainerror

import (
	"errors"
	"net/http"
	"strings"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
	"github.com/fancyqqq/tripgo-avito/internal/generated"
)

type invalidRequestError struct {
	err error
}

func (e *invalidRequestError) Error() string {
	return e.err.Error()
}

func (e *invalidRequestError) Unwrap() error {
	return e.err
}

func InvalidRequest(err error) error {
	return &invalidRequestError{err: err}
}

func Problem(err error) (int, generated.Problem) {
	var validationErr *trip.ValidationError
	var requestErr *invalidRequestError

	switch {
	case errors.As(err, &validationErr):
		return newProblem(http.StatusBadRequest, "invalid_request", "Invalid request", validationErr.Error())
	case errors.As(err, &requestErr):
		return newProblem(http.StatusBadRequest, "invalid_request", "Invalid request", requestErr.Error())
	case errors.Is(err, trip.ErrTripNotFound):
		return newProblem(http.StatusNotFound, "trip_not_found", "Trip not found", "Trip was not found")
	case errors.Is(err, trip.ErrTripCompleted):
		return newProblem(http.StatusConflict, "trip_completed", "Trip completed", "Operation is not allowed for a completed trip")
	case errors.Is(err, trip.ErrDriverBusy):
		return newProblem(http.StatusConflict, "driver_busy", "Driver busy", "Driver already has an active trip")
	default:
		return newProblem(http.StatusInternalServerError, "internal_error", "Internal Server Error", "Internal server error")
	}
}

func newProblem(status int, code, title, detail string) (int, generated.Problem) {
	return status, generated.Problem{
		Type:   "https://tripgo.example/problems/" + strings.ReplaceAll(code, "_", "-"),
		Title:  title,
		Status: int32(status),
		Detail: &detail,
		Code:   code,
	}
}
