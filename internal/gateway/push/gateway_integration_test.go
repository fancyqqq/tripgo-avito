//go:build integration

package push

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRequestPositionIntegration(t *testing.T) {
	endpoint := os.Getenv("PUSH_HTTP_URL")
	if endpoint == "" {
		t.Fatal("PUSH_HTTP_URL is not set, run via make test-integration")
	}
	gateway := newGateway(t, endpoint, time.Second)
	if err := gateway.RequestPosition(context.Background(), uuid.New(), uuid.New()); err != nil {
		t.Fatalf("request position from local Push Service: %v", err)
	}
}
