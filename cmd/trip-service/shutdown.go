package main

import (
	"context"
	"errors"
	"fmt"
)

func shutdown(ctx context.Context, server httpServer, scanner, workers backgroundWorker, closeResources func()) error {
	var httpErr, scannerErr, workersErr error
	if err := server.Shutdown(ctx); err != nil {
		httpErr = fmt.Errorf("shutdown HTTP server: %w", err)
		if err := server.Close(); err != nil {
			httpErr = errors.Join(httpErr, fmt.Errorf("force close HTTP server: %w", err))
		}
	}
	if err := scanner.Shutdown(ctx); err != nil {
		scannerErr = fmt.Errorf("shutdown position scanner: %w", err)
	}
	scanner.Close()
	if err := workers.Shutdown(ctx); err != nil {
		workersErr = fmt.Errorf("shutdown position workers: %w", err)
	}
	workers.Close()
	closeResources()
	return errors.Join(httpErr, scannerErr, workersErr)
}
