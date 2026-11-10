package get

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
)

type UseCase struct {
	repo tripRepository
}

func New(repo tripRepository) *UseCase {
	return &UseCase{repo: repo}
}

func (u *UseCase) Execute(ctx context.Context, id uuid.UUID) (trip.Trip, error) {
	t, err := u.repo.GetByID(ctx, id)
	if err != nil {
		return trip.Trip{}, fmt.Errorf("get trip: %w", err)
	}
	return t, nil
}
