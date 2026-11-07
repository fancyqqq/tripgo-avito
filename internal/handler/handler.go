package handler

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/fancyqqq/tripgo-avito/api"
	"github.com/fancyqqq/tripgo-avito/internal/domain"
	"github.com/google/uuid"
)

type Pinger interface {
	Ping(context.Context) error
}

type TxManager interface {
	Do(context.Context, func(context.Context) error) error
}

type TripRepository interface {
	Create(context.Context, domain.Trip) error
	GetByID(context.Context, uuid.UUID) (domain.Trip, error)
	Finish(context.Context, uuid.UUID, time.Time) (domain.Trip, error)
}

type HistoryRepository interface {
	Add(context.Context, domain.StatusChange) error
}

type Handler struct {
	pinger       Pinger
	queryTimeout time.Duration
	txManager    TxManager
	trips        TripRepository
	history      HistoryRepository
}

func New(
	pinger Pinger,
	queryTimeout time.Duration,
	txManager TxManager,
	trips TripRepository,
	history HistoryRepository,
) *Handler {
	return &Handler{
		pinger:       pinger,
		queryTimeout: queryTimeout,
		txManager:    txManager,
		trips:        trips,
		history:      history,
	}
}

var _ api.ServerInterface = (*Handler)(nil)

func InvalidRequest(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("invalid request: %v", err)
	writeProblem(w, r, http.StatusBadRequest, "invalid_request", "Invalid request", "Request parameters are invalid")
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("write response: %v", err)
	}
}

func writeProblem(w http.ResponseWriter, r *http.Request, status int, code, title, detail string) {
	instance := r.URL.Path

	body := api.Problem{
		Type:     "https://tripgo.example/problems/" + strings.ReplaceAll(code, "_", "-"),
		Title:    title,
		Status:   int32(status),
		Detail:   &detail,
		Instance: &instance,
		Code:     code,
	}

	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("write problem: %v", err)
	}
}

func toAPITrip(t domain.Trip) api.Trip {
	return api.Trip{
		Id:       t.ID,
		UserId:   t.UserID,
		DriverId: t.DriverID,
		StartPoint: api.Coordinates{
			Latitude:  t.StartPoint.Latitude,
			Longitude: t.StartPoint.Longitude,
		},
		EndPoint: api.Coordinates{
			Latitude:  t.EndPoint.Latitude,
			Longitude: t.EndPoint.Longitude,
		},
		Price:      t.Price,
		Status:     api.TripStatus(t.Status),
		StartedAt:  t.StartedAt,
		FinishedAt: t.FinishedAt,
	}
}
