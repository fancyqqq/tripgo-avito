package handler

import (
	"net/http"

	"github.com/fancyqqq/tripgo-avito/api"
)

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, api.HealthResponse{Status: api.Ok})
}
