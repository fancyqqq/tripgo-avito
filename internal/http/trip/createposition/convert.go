package createposition

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"

	"github.com/google/uuid"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
	"github.com/fancyqqq/tripgo-avito/internal/generated"
	"github.com/fancyqqq/tripgo-avito/internal/model"
)

const maxBodySize = 1 << 20

func parseRequest(r *http.Request, tripID uuid.UUID) (model.CreatePosition, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodySize+1))
	if err != nil {
		return model.CreatePosition{}, errors.New("cannot read request body")
	}
	if len(body) > maxBodySize {
		return model.CreatePosition{}, errors.New("request body is too large")
	}
	fields, err := requireFields(body, "", "coordinates", "recorded_at")
	if err != nil {
		return model.CreatePosition{}, err
	}
	if _, err := requireFields(fields["coordinates"], "coordinates", "latitude", "longitude"); err != nil {
		return model.CreatePosition{}, err
	}
	var request generated.CreateTripPositionJSONRequestBody
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return model.CreatePosition{}, err
	}
	return model.CreatePosition{
		TripID:      tripID,
		Coordinates: trip.Point{Latitude: request.Coordinates.Latitude, Longitude: request.Coordinates.Longitude},
		RecordedAt:  request.RecordedAt,
	}, nil
}

func requireFields(data []byte, prefix string, names ...string) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, errors.New("request body and coordinates must be JSON objects")
	}
	for name := range fields {
		if !slices.Contains(names, name) {
			return nil, fmt.Errorf("unknown field %q", name)
		}
	}
	for _, name := range names {
		if value, ok := fields[name]; !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			if prefix != "" {
				name = prefix + "." + name
			}
			return nil, fmt.Errorf("%s is required", name)
		}
	}
	return fields, nil
}

func toResponse(position trip.Position) generated.TripPosition {
	return generated.TripPosition{
		Id:          position.ID,
		TripId:      position.TripID,
		Coordinates: generated.Coordinates{Latitude: position.Coordinates.Latitude, Longitude: position.Coordinates.Longitude},
		RecordedAt:  position.RecordedAt,
	}
}
