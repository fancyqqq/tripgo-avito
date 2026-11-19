package telemetry

import (
	"context"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
)

const (
	serviceVersionAttribute  = "service.version"
	serviceInstanceAttribute = "service.instance.id"
)

func newResource(ctx context.Context) (*resource.Resource, error) {
	return resource.New(ctx,
		resource.WithAttributes(
			attribute.String(serviceVersionAttribute, "dev"),
			attribute.String(serviceInstanceAttribute, uuid.NewString()),
		),
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
	)
}
