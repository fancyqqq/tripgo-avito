package middleware

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/metric/noop"
)

func Tracing(routes chi.Routes, opts ...otelhttp.Option) Middleware {
	opts = append([]otelhttp.Option{otelhttp.WithMeterProvider(noop.NewMeterProvider())}, opts...)

	return func(next http.Handler) http.Handler {
		traced := otelhttp.NewHandler(next, "http.server", opts...)

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			traced.ServeHTTP(w, withRoutePattern(routes, r))
		})
	}
}

func withRoutePattern(routes chi.Routes, r *http.Request) *http.Request {
	rctx := chi.NewRouteContext()
	if !routes.Match(rctx, r.Method, r.URL.Path) {
		return r
	}

	routed := new(http.Request)
	*routed = *r
	routed.Pattern = rctx.RoutePattern()
	return routed
}
