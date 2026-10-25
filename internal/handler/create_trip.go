package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/fancyqqq/tripgo-avito/api"
	"github.com/fancyqqq/tripgo-avito/internal/domain"
	"github.com/google/uuid"
)

func (h *Handler) CreateTrip(w http.ResponseWriter, r *http.Request, params api.CreateTripParams) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_request", "Invalid request", "cannot read request body")
		return
	}

	if err := checkRequiredFields(body); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_request", "Invalid request", err.Error())
		return
	}

	var req api.CreateTripJSONRequestBody
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_request", "Invalid request", err.Error())
		return
	}

	if err := validateTripData(req); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "invalid_request", "Invalid request", err.Error())
		return
	}

	trip := domain.Trip{
		ID:       uuid.New(),
		UserID:   req.UserId,
		DriverID: req.DriverId,
		StartPoint: domain.Point{
			Latitude:  req.StartPoint.Latitude,
			Longitude: req.StartPoint.Longitude,
		},
		EndPoint: domain.Point{
			Latitude:  req.EndPoint.Latitude,
			Longitude: req.EndPoint.Longitude,
		},
		Price:     req.Price,
		Status:    domain.StatusActive,
		StartedAt: time.Now().UTC().Truncate(time.Microsecond),
	}

	err = h.txManager.Do(r.Context(), func(ctx context.Context) error {
		if err := h.trips.Create(ctx, trip); err != nil {
			return err
		}
		return h.history.Add(ctx, domain.StatusChange{
			TripID:   trip.ID,
			ToStatus: domain.StatusActive,
			Reason:   "trip created",
		})
	})
	if errors.Is(err, domain.ErrDriverBusy) {
		writeProblem(w, r, http.StatusConflict, "driver_busy", "Driver busy", "Driver already has an active trip")
		return
	}
	if err != nil {
		log.Printf("create trip: %v", err)
		writeProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal Server Error", "Internal server error")
		return
	}

	w.Header().Set("Location", "/api/v1/trips/"+trip.ID.String())
	writeJSON(w, http.StatusCreated, toAPITrip(trip))
}

func validateTripData(req api.TripData) error {
	if req.UserId == uuid.Nil {
		return fmt.Errorf("user_id must not be empty")
	}
	if req.DriverId == uuid.Nil {
		return fmt.Errorf("driver_id must not be empty")
	}
	if err := validateCoordinates("start_point", req.StartPoint); err != nil {
		return err
	}
	if err := validateCoordinates("end_point", req.EndPoint); err != nil {
		return err
	}
	if req.Price < 0 {
		return fmt.Errorf("price must be >= 0")
	}

	return nil
}

func validateCoordinates(field string, c api.Coordinates) error {
	if c.Latitude < -90 || c.Latitude > 90 {
		return fmt.Errorf("%s.latitude must be between -90 and 90", field)
	}
	if c.Longitude < -180 || c.Longitude > 180 {
		return fmt.Errorf("%s.longitude must be between -180 and 180", field)
	}
	return nil
}

func checkRequiredFields(body []byte) error {
	top, err := requireFields(body, "", "user_id", "driver_id", "start_point", "end_point", "price")
	if err != nil {
		return err
	}
	for _, p := range []string{"start_point", "end_point"} {
		if _, err := requireFields(top[p], p+".", "latitude", "longitude"); err != nil {
			return err
		}
	}
	return nil
}

func requireFields(data []byte, prefix string, fields ...string) (map[string]json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, fmt.Errorf("%smust be a JSON object", prefix)
	}
	for _, f := range fields {
		v, ok := obj[f]
		if !ok || string(v) == "null" {
			return nil, fmt.Errorf("%s%s is required", prefix, f)
		}
	}
	return obj, nil
}
