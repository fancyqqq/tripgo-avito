//go:generate go tool mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}
package main

import "context"

type (
	httpServer interface {
		Shutdown(ctx context.Context) error
		Close() error
	}
	backgroundWorker interface {
		Shutdown(ctx context.Context) error
		Close()
	}
)
