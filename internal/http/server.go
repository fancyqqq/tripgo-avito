package http

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/fancyqqq/tripgo-avito/internal/generated"
	"github.com/fancyqqq/tripgo-avito/internal/http/domainerror"
	"github.com/fancyqqq/tripgo-avito/internal/http/health"
	"github.com/fancyqqq/tripgo-avito/internal/http/response"
	"github.com/fancyqqq/tripgo-avito/internal/http/trip/create"
	"github.com/fancyqqq/tripgo-avito/internal/http/trip/finish"
	"github.com/fancyqqq/tripgo-avito/internal/http/trip/get"
)

type Server struct {
	create *create.Handler
	get    *get.Handler
	finish *finish.Handler
	health *health.Handler
	logger *slog.Logger
}

var _ generated.ServerInterface = (*Server)(nil)

func NewServer(
	createHandler *create.Handler,
	getHandler *get.Handler,
	finishHandler *finish.Handler,
	healthHandler *health.Handler,
	logger *slog.Logger,
) *Server {
	return &Server{
		create: createHandler,
		get:    getHandler,
		finish: finishHandler,
		health: healthHandler,
		logger: logger,
	}
}

func (s *Server) Routes() http.Handler {
	return generated.HandlerWithOptions(s, generated.ChiServerOptions{
		BaseRouter:       chi.NewRouter(),
		ErrorHandlerFunc: s.paramError,
	})
}

func (s *Server) CreateTrip(w http.ResponseWriter, r *http.Request, _ generated.CreateTripParams) {
	s.create.Handle(w, r)
}

func (s *Server) GetTrip(w http.ResponseWriter, r *http.Request, tripID generated.TripId) {
	s.get.Handle(w, r, tripID)
}

func (s *Server) FinishTrip(w http.ResponseWriter, r *http.Request, tripID generated.TripId) {
	s.finish.Handle(w, r, tripID)
}

func (s *Server) Health(w http.ResponseWriter, r *http.Request) {
	s.health.Health(w, r)
}

func (s *Server) Ready(w http.ResponseWriter, r *http.Request) {
	s.health.Ready(w, r)
}

func (s *Server) paramError(w http.ResponseWriter, r *http.Request, err error) {
	response.Error(w, r, s.logger, domainerror.InvalidRequest(err))
}
