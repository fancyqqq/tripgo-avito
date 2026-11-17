package listpositions

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
)

type UseCase struct {
	trips     tripRepository
	positions positionRepository
}

func New(trips tripRepository, positions positionRepository) *UseCase {
	return &UseCase{trips: trips, positions: positions}
}

func (u *UseCase) Execute(ctx context.Context, id uuid.UUID) ([]trip.Position, error) {
	if _, err := u.trips.GetByID(ctx, id); err != nil {
		return nil, fmt.Errorf("get trip for route: %w", err)
	}
	positions, err := u.positions.ListByTripID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list trip positions: %w", err)
	}
	return positions, nil
}
