package main

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"go.uber.org/mock/gomock"
)

func TestShutdownOrderAndErrors(t *testing.T) {
	t.Parallel()
	httpFailure := errors.New("HTTP shutdown failed")
	closeFailure := errors.New("HTTP close failed")
	scannerFailure := errors.New("scanner shutdown failed")
	workersFailure := errors.New("workers shutdown failed")
	for _, tc := range []struct {
		name                                      string
		httpErr, closeErr, scannerErr, workersErr error
	}{
		{name: "graceful shutdown"},
		{name: "HTTP timeout", httpErr: context.DeadlineExceeded},
		{name: "scanner timeout", scannerErr: context.DeadlineExceeded},
		{name: "workers timeout", workersErr: context.DeadlineExceeded},
		{name: "all errors preserved", httpErr: httpFailure, closeErr: closeFailure, scannerErr: scannerFailure, workersErr: workersFailure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			server, scanner, workers := NewMockhttpServer(ctrl), NewMockbackgroundWorker(ctrl), NewMockbackgroundWorker(ctrl)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			calls := []*gomock.Call{server.EXPECT().Shutdown(ctx).Return(tc.httpErr)}
			if tc.httpErr != nil {
				calls = append(calls, server.EXPECT().Close().Return(tc.closeErr))
			}
			calls = append(calls,
				scanner.EXPECT().Shutdown(ctx).Return(tc.scannerErr),
				scanner.EXPECT().Close(),
				workers.EXPECT().Shutdown(ctx).Return(tc.workersErr),
				workers.EXPECT().Close(),
			)
			for i := 1; i < len(calls); i++ {
				calls[i].After(calls[i-1])
			}
			closed := false
			last := calls[len(calls)-1]
			last.Do(func() { closed = true })
			resourcesClosed := false
			err := shutdown(ctx, server, scanner, workers, func() {
				if !closed {
					t.Error("resources closed before workers joined")
				}
				resourcesClosed = true
			})
			if !resourcesClosed {
				t.Fatal("resources were not closed")
			}
			if tc.httpErr == nil && tc.scannerErr == nil && tc.workersErr == nil && err != nil {
				t.Fatalf("graceful shutdown: %v", err)
			}
			for _, want := range []error{tc.httpErr, tc.closeErr, tc.scannerErr, tc.workersErr} {
				if want != nil && !errors.Is(err, want) {
					t.Errorf("shutdown lost %v: %v", want, err)
				}
			}
		})
	}
}

func TestShutdownReusesCancelledBudgetForRemainingPhases(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	server, scanner, workers := NewMockhttpServer(ctrl), NewMockbackgroundWorker(ctrl), NewMockbackgroundWorker(ctrl)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gomock.InOrder(
		server.EXPECT().Shutdown(ctx).DoAndReturn(func(context.Context) error {
			cancel()
			return ctx.Err()
		}),
		server.EXPECT().Close().Return(nil),
		scanner.EXPECT().Shutdown(ctx).DoAndReturn(func(ctx context.Context) error { return ctx.Err() }),
		scanner.EXPECT().Close(),
		workers.EXPECT().Shutdown(ctx).DoAndReturn(func(ctx context.Context) error { return ctx.Err() }),
		workers.EXPECT().Close(),
	)
	closed := false
	if err := shutdown(ctx, server, scanner, workers, func() { closed = true }); !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown: %v", err)
	}
	if !closed {
		t.Fatal("expired budget skipped resource cleanup")
	}
}

func TestTelemetryShutdownReceivesExpiredBudget(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithDeadline(context.Background(), time.Unix(1, 0))
	defer cancel()
	called := false
	flushTelemetry(ctx, slog.New(slog.DiscardHandler), func(actual context.Context) error {
		called = true
		if actual != ctx || !errors.Is(actual.Err(), context.DeadlineExceeded) {
			t.Errorf("telemetry received a new budget: %v", actual.Err())
		}
		return actual.Err()
	})
	if !called {
		t.Fatal("telemetry cleanup was skipped")
	}
}
