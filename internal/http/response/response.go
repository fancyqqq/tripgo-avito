package response

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/fancyqqq/tripgo-avito/internal/http/domainerror"
)

func JSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func Error(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	status, problem := domainerror.Problem(err)
	if status >= http.StatusInternalServerError {
		logger.ErrorContext(r.Context(), "request failed", "error", err)
		span := trace.SpanFromContext(r.Context())
		span.RecordError(err)
		span.SetStatus(codes.Error, problem.Code)
	}

	instance := r.URL.Path
	problem.Instance = &instance

	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problem)
}
