package create

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
	"github.com/fancyqqq/tripgo-avito/internal/generated"
	"github.com/fancyqqq/tripgo-avito/internal/model"
)

const maxBodySize = 1 << 20

func parseRequest(r *http.Request) (model.CreateTrip, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodySize))
	if err != nil {
		return model.CreateTrip{}, errors.New("cannot read request body")
	}

	if err := checkRequiredFields(body); err != nil {
		return model.CreateTrip{}, err
	}

	var req generated.CreateTripJSONRequestBody
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return model.CreateTrip{}, err
	}

	return model.CreateTrip{
		UserID:   req.UserId,
		DriverID: req.DriverId,
		Start:    trip.Point{Latitude: req.StartPoint.Latitude, Longitude: req.StartPoint.Longitude},
		End:      trip.Point{Latitude: req.EndPoint.Latitude, Longitude: req.EndPoint.Longitude},
		Price:    req.Price,
	}, nil
}

func checkRequiredFields(body []byte) error {
	top, err := requireFields(body, "", "user_id", "driver_id", "start_point", "end_point", "price")
	if err != nil {
		return err
	}
	for _, point := range []string{"start_point", "end_point"} {
		if _, err := requireFields(top[point], point, "latitude", "longitude"); err != nil {
			return err
		}
	}
	return nil
}

func requireFields(data []byte, name string, fields ...string) (map[string]json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		if name == "" {
			return nil, errors.New("request body must be a JSON object")
		}
		return nil, fmt.Errorf("%s must be a JSON object", name)
	}

	for _, f := range fields {
		path := f
		if name != "" {
			path = name + "." + f
		}
		if v, ok := obj[f]; !ok || string(v) == "null" {
			return nil, fmt.Errorf("%s is required", path)
		}
	}
	return obj, nil
}

func toResponse(t trip.Trip) generated.Trip {
	return generated.Trip{
		Id:         t.ID,
		UserId:     t.UserID,
		DriverId:   t.DriverID,
		StartPoint: generated.Coordinates{Latitude: t.StartPoint.Latitude, Longitude: t.StartPoint.Longitude},
		EndPoint:   generated.Coordinates{Latitude: t.EndPoint.Latitude, Longitude: t.EndPoint.Longitude},
		Price:      t.Price,
		Status:     generated.TripStatus(t.Status),
		StartedAt:  t.StartedAt,
		FinishedAt: t.FinishedAt,
	}
}
