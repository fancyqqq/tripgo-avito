package get

import (
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/fancyqqq/tripgo-avito/internal/http/response"
)

type Handler struct {
	uc     useCase
	logger *slog.Logger
}

func New(uc useCase, logger *slog.Logger) *Handler {
	return &Handler{uc: uc, logger: logger}
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	t, err := h.uc.Execute(r.Context(), id)
	if err != nil {
		response.Error(w, r, h.logger, err)
		return
	}

	response.JSON(w, http.StatusOK, toResponse(t))
}
