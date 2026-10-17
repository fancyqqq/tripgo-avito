package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/fancyqqq/tripgo-avito/api"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	pool         *pgxpool.Pool
	queryTimeout time.Duration
	api.Unimplemented
}

func New(pool *pgxpool.Pool, queryTimeout time.Duration) *Handler {
	return &Handler{pool: pool, queryTimeout: queryTimeout}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("write response: %v", err)
	}
}
