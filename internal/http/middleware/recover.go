package middleware

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/fancyqqq/tripgo-avito/internal/http/response"
)

func Recover(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				p := recover()
				if p == nil {
					return
				}
				if err, ok := p.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(p)
				}
				response.Error(w, r, logger, fmt.Errorf("panic: %v\n%s", p, debug.Stack()))
			}()

			next.ServeHTTP(w, r)
		})
	}
}
