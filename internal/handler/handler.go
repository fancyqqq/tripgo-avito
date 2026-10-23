package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/fancyqqq/tripgo-avito/api"
	"github.com/fancyqqq/tripgo-avito/internal/domain"
	"github.com/fancyqqq/tripgo-avito/internal/postgres"
	"github.com/fancyqqq/tripgo-avito/internal/repository"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	pool         *pgxpool.Pool
	queryTimeout time.Duration
	txManager    *postgres.TxManager
	trips        *repository.TripRepository
	history      *repository.HistoryRepository
	api.Unimplemented
}

func New(
	pool *pgxpool.Pool,
	queryTimeout time.Duration,
	txManager *postgres.TxManager,
	trips *repository.TripRepository,
	history *repository.HistoryRepository,
) *Handler {
	return &Handler{
		pool:         pool,
		queryTimeout: queryTimeout,
		txManager:    txManager,
		trips:        trips,
		history:      history,
	}
}

func InvalidRequest(w http.ResponseWriter, r *http.Request, err error) {
	writeProblem(w, r, http.StatusBadRequest, "invalid_request", "Invalid request", err.Error())
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
