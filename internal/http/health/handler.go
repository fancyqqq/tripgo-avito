package health

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/fancyqqq/tripgo-avito/internal/generated"
	"github.com/fancyqqq/tripgo-avito/internal/http/response"
)

type Handler struct {
	db      pinger
	timeout time.Duration
	logger  *slog.Logger
}

func New(db pinger, timeout time.Duration, logger *slog.Logger) *Handler {
	return &Handler{db: db, timeout: timeout, logger: logger}
}

func (h *Handler) Health(w http.ResponseWriter, _ *http.Request) {
	response.JSON(w, http.StatusOK, generated.HealthResponse{Status: generated.Ok})
}

func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()

	if err := h.db.Ping(ctx); err != nil {
		h.logger.WarnContext(r.Context(), "database is unavailable", "error", err)
		response.JSON(w, http.StatusServiceUnavailable, generated.HealthResponse{Status: generated.Unavailable})
		return
	}

	response.JSON(w, http.StatusOK, generated.HealthResponse{Status: generated.Ok})
}
