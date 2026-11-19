package telemetry

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
)

func resourceAttribute(res *resource.Resource, key string) string {
	value, _ := res.Set().Value(attribute.Key(key))
	return value.AsString()
}

func TestResourceHasDistinctProcessInstances(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "trip-service")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "service.namespace=tripgo,deployment.environment=local")
	first, err := newResource(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := newResource(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"service.name": "trip-service", "service.namespace": "tripgo", "deployment.environment": "local", serviceVersionAttribute: "dev"} {
		if got := resourceAttribute(first, key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	firstID, err := uuid.Parse(resourceAttribute(first, serviceInstanceAttribute))
	if err != nil || firstID == uuid.Nil {
		t.Fatalf("invalid instance UUID: %v", err)
	}
	if resourceAttribute(first, serviceInstanceAttribute) == resourceAttribute(second, serviceInstanceAttribute) {
		t.Fatal("two application instances share an identifier")
	}
}

func TestResourceStandardEnvironmentOverridesBuildAndInstanceDefaults(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "trip-service")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "service.version=lab3,service.instance.id=load-check")
	res, err := newResource(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if resourceAttribute(res, serviceVersionAttribute) != "lab3" || resourceAttribute(res, serviceInstanceAttribute) != "load-check" {
		t.Fatalf("standard OTel overrides not preserved: %v", res.Attributes())
	}
}
