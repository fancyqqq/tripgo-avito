package middleware

import (
	"log/slog"
	"net/http"
	"time"
)

func Logging(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := newStatusRecorder(w)

			next.ServeHTTP(rec, r)

			level := slog.LevelInfo
			if rec.status >= http.StatusInternalServerError {
				level = slog.LevelError
			}

			logger.LogAttrs(r.Context(), level, "http request",
				slog.String("method", r.Method),
				slog.String("route", r.Pattern),
				slog.String("path", r.URL.Path),
				slog.Int("status", rec.status),
				slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
			)
		})
	}
}
