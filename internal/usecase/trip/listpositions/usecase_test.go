package listpositions_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/fancyqqq/tripgo-avito/internal/domain/trip"
	"github.com/fancyqqq/tripgo-avito/internal/usecase/trip/listpositions"
)

func TestExecute(t *testing.T) {
	t.Parallel()
	errStorage := errors.New("storage unavailable")
	id := uuid.New()
	points := []trip.Position{{ID: 1, TripID: id, RecordedAt: time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)}}
	tests := []struct {
		name        string
		status      trip.Status
		tripErr     error
		positions   []trip.Position
		positionErr error
		wantErr     error
	}{
		{name: "active trip", status: trip.StatusActive, positions: points},
		{name: "completed trip", status: trip.StatusCompleted, positions: points},
		{name: "empty route", status: trip.StatusActive, positions: []trip.Position{}},
		{name: "trip missing", tripErr: trip.ErrTripNotFound, wantErr: trip.ErrTripNotFound},
		{name: "trip query fails", tripErr: errStorage, wantErr: errStorage},
		{name: "positions query fails", status: trip.StatusActive, positionErr: errStorage, wantErr: errStorage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			trips := NewMocktripRepository(ctrl)
			positions := NewMockpositionRepository(ctrl)
			tripCall := trips.EXPECT().GetByID(gomock.Any(), id).Return(trip.Trip{ID: id, Status: tt.status}, tt.tripErr)
			if tt.tripErr == nil {
				positions.EXPECT().ListByTripID(gomock.Any(), id).After(tripCall).Return(tt.positions, tt.positionErr)
			}
			got, err := listpositions.New(trips, positions).Execute(context.Background(), id)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				if got != nil {
					t.Fatalf("positions on error = %+v", got)
				}
				return
			}
			if !reflect.DeepEqual(got, tt.positions) {
				t.Fatalf("positions = %+v, want %+v", got, tt.positions)
			}
		})
	}
}
