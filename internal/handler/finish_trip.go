package handler

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/fancyqqq/tripgo-avito/internal/domain"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

func (h *Handler) FinishTrip(w http.ResponseWriter, r *http.Request, tripId openapi_types.UUID) {
	finishedAt := time.Now().UTC().Truncate(time.Microsecond)
	var trip domain.Trip
	from := domain.StatusActive

	err := h.txManager.Do(r.Context(), func(ctx context.Context) error {
		var err error
		trip, err = h.trips.Finish(ctx, tripId, finishedAt)
		if err != nil {
			return err
		}
		return h.history.Add(ctx, domain.StatusChange{
			TripID:     tripId,
			FromStatus: &from,
			ToStatus:   domain.StatusCompleted,
			Reason:     "trip completed",
		})
	})
	if errors.Is(err, domain.ErrTripNotFound) {
		writeProblem(w, r, http.StatusNotFound, "trip_not_found", "Trip not found", "Trip was not found")
		return
	}
	if errors.Is(err, domain.ErrTripCompleted) {
		writeProblem(w, r, http.StatusConflict, "trip_completed", "Trip completed", "Operation is not allowed for a completed trip")
		return
	}
	if err != nil {
		log.Printf("finish trip: %v", err)
		writeProblem(
			w,
			r,
			http.StatusInternalServerError,
			"internal_error",
			"Internal Server Error",
			"Internal server error",
		)
		return
	}

	writeJSON(w, http.StatusOK, toAPITrip(trip))
}
