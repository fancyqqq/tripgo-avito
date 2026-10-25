package handler

import (
	"errors"
	"log"
	"net/http"

	"github.com/fancyqqq/tripgo-avito/internal/domain"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

func (h *Handler) GetTrip(w http.ResponseWriter, r *http.Request, tripId openapi_types.UUID) {
	trip, err := h.trips.GetByID(r.Context(), tripId)
	if errors.Is(err, domain.ErrTripNotFound) {
		writeProblem(w, r, http.StatusNotFound, "trip_not_found", "Trip not found", "Trip was not found")
		return
	}
	if err != nil {
		log.Printf("get trip: %v", err)
		writeProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal Server Error", "Internal server error")
		return
	}
	writeJSON(w, http.StatusOK, toAPITrip(trip))
}
