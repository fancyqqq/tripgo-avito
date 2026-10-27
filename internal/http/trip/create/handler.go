package create

import (
	"log/slog"
	"net/http"

	"github.com/fancyqqq/tripgo-avito/internal/http/domainerror"
	"github.com/fancyqqq/tripgo-avito/internal/http/response"
)

type Handler struct {
	uc     useCase
	logger *slog.Logger
}

func New(uc useCase, logger *slog.Logger) *Handler {
	return &Handler{uc: uc, logger: logger}
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	in, err := parseRequest(r)
	if err != nil {
		response.Error(w, r, h.logger, domainerror.InvalidRequest(err))
		return
	}

	t, err := h.uc.Execute(r.Context(), in)
	if err != nil {
		response.Error(w, r, h.logger, err)
		return
	}

	w.Header().Set("Location", "/api/v1/trips/"+t.ID.String())
	response.JSON(w, http.StatusCreated, toResponse(t))
}
