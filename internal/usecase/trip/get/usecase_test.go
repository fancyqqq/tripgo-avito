package get_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
	"github.com/fancyqqq/tripgo-avito/internal/usecase/trip/get"
)

func TestExecute(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	stored := trip.Trip{
		ID:        id,
		UserID:    uuid.New(),
		DriverID:  uuid.New(),
		Price:     500,
		Status:    trip.StatusActive,
		StartedAt: time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC),
	}
	errDB := errors.New("db is down")

	tests := []struct {
		name     string
		repoTrip trip.Trip
		repoErr  error
		want     trip.Trip
		wantErr  error
	}{
		{
			name:     "found",
			repoTrip: stored,
			want:     stored,
		},
		{
			name:    "not found",
			repoErr: trip.ErrTripNotFound,
			wantErr: trip.ErrTripNotFound,
		},
		{
			name:    "repository error",
			repoErr: errDB,
			wantErr: errDB,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			repo := NewMocktripRepository(ctrl)
			repo.EXPECT().GetByID(gomock.Any(), id).Return(tt.repoTrip, tt.repoErr)

			got, err := get.New(repo).Execute(context.Background(), id)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("trip = %+v, want %+v", got, tt.want)
			}
		})
	}
}
