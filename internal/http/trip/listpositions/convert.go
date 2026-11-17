package listpositions

import (
	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
	"github.com/fancyqqq/tripgo-avito/internal/generated"
)

func toResponse(positions []trip.Position) generated.TripPositionsResponse {
	result := make([]generated.TripPosition, len(positions))
	for i, position := range positions {
		result[i] = generated.TripPosition{
			Id:          position.ID,
			TripId:      position.TripID,
			Coordinates: generated.Coordinates{Latitude: position.Coordinates.Latitude, Longitude: position.Coordinates.Longitude},
			RecordedAt:  position.RecordedAt,
		}
	}
	return generated.TripPositionsResponse{Positions: result}
}
