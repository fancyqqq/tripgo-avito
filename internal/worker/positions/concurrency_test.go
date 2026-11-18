package positions_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/fancyqqq/tripgo-avito/internal/model"
	"github.com/fancyqqq/tripgo-avito/internal/worker/positions"
)

func TestConcurrentEnqueueCancelAndProcessing(t *testing.T) {
	t.Parallel()
	const workers, publishers, iterations = 4, 12, 100
	f := newFixture(t, poolConfig(workers, 16), nil)
	trips := make([]model.StaleTrip, 8)
	for i := range trips {
		trips[i] = candidate()
	}
	var mu sync.Mutex
	running := make(map[uuid.UUID]bool)
	f.gateway.EXPECT().RequestPosition(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(func(_ context.Context, id, _ uuid.UUID) error {
		mu.Lock()
		if running[id] {
			t.Errorf("simultaneous requests for trip %s", id)
		}
		running[id] = true
		if len(running) > workers {
			t.Errorf("running requests = %d, workers = %d", len(running), workers)
		}
		mu.Unlock()
		defer func() {
			mu.Lock()
			delete(running, id)
			mu.Unlock()
		}()
		return nil
	})
	f.metrics.EXPECT().RequestFinished(gomock.Any(), success, gomock.Any()).AnyTimes()
	f.metrics.EXPECT().RequestDropped(gomock.Any()).AnyTimes()
	start(t, f.pool, context.Background())
	barrier := make(chan struct{})
	var wg sync.WaitGroup
	for publisher := range publishers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-barrier
			for i := range iterations {
				trip := trips[(publisher+i)%len(trips)]
				if _, err := f.pool.Enqueue(context.Background(), trip); err != nil {
					t.Errorf("Enqueue: %v", err)
				}
				if i%2 == 0 {
					f.pool.Cancel(trip.TripID)
				}
				if state := f.pool.Snapshot(); state.ActiveWorkers > workers || state.QueueSize > 16 || state.Pending > 16+workers {
					t.Errorf("pool exceeded bounds: %+v", state)
				}
			}
		}()
	}
	close(barrier)
	wg.Wait()
	shutdown(t, f.pool)
}

func TestConcurrentEnqueueAndShutdownDrainsAcceptedTasks(t *testing.T) {
	t.Parallel()
	f := newFixture(t, poolConfig(4, 16), nil)
	var accepted, finished atomic.Int64
	f.metrics.EXPECT().RequestDropped(gomock.Any()).AnyTimes()
	f.gateway.EXPECT().RequestPosition(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(nil)
	f.metrics.EXPECT().RequestFinished(gomock.Any(), success, gomock.Any()).AnyTimes().Do(func(context.Context, string, time.Duration) { finished.Add(1) })
	enqueue(t, f.pool, candidate(), positions.Queued)
	accepted.Add(1)
	start(t, f.pool, context.Background())
	barrier := make(chan struct{})
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-barrier
			for range 64 {
				result, err := f.pool.Enqueue(context.Background(), candidate())
				if err != nil && !errors.Is(err, positions.ErrStopped) {
					t.Errorf("Enqueue: %v", err)
				}
				if result == positions.Queued {
					accepted.Add(1)
				}
			}
		}()
	}
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-barrier
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := f.pool.Shutdown(ctx); err != nil {
				t.Errorf("Shutdown: %v", err)
			}
		}()
	}
	close(barrier)
	wg.Wait()
	if finished.Load() != accepted.Load() {
		t.Fatalf("finished = %d, accepted = %d", finished.Load(), accepted.Load())
	}
	shutdown(t, f.pool)
}
