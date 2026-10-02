package handler

import (
	"encoding/json"
	"net/http"

	"github.com/fancyqqq/tripgo-avito/api"
)

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {

	w.Header().Set("Content-Type", "application/json")

	response := api.HealthResponse{
		Status: api.Ok,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		return
	}
}
