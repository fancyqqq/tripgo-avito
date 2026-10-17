package handler

import (
	"context"
	"log"
	"net/http"

	"github.com/fancyqqq/tripgo-avito/api"
)

func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), h.queryTimeout)
	defer cancel()

	w.Header().Set("Content-Type", "application/json")

	if err := h.pool.Ping(ctx); err != nil {
		log.Printf("ping database: %v", err)
		writeJSON(w, http.StatusServiceUnavailable, api.HealthResponse{Status: api.Unavailable})
		return
	}

	writeJSON(w, http.StatusOK, api.HealthResponse{Status: api.Ok})
}
