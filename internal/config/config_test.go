package config_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/fancyqqq/tripgo-avito/internal/config"
)

const (
	checkIntervalEnv      = "POSITION_CHECK_INTERVAL"
	staleAfterEnv         = "POSITION_STALE_AFTER"
	workersEnv            = "POSITION_WORKERS"
	queueSizeEnv          = "POSITION_QUEUE_SIZE"
	pushURLEnv            = "PUSH_HTTP_URL"
	pushTimeoutEnv        = "PUSH_REQUEST_TIMEOUT"
	validationErrorPrefix = "validate config:"
	parseErrorPrefix      = "parse config:"
	negativeDuration      = "-1s"
)

func setTestEnvironment(t *testing.T) {
	t.Helper()

	values := map[string]string{
		"OTEL_SERVICE_NAME":          "trip-service",
		"LOG_LEVEL":                  "info",
		"SHUTDOWN_TIMEOUT":           "10s",
		"HTTP_ADDR":                  ":8080",
		"HTTP_READ_TIMEOUT":          "",
		"HTTP_READ_HEADER_TIMEOUT":   "",
		"HTTP_WRITE_TIMEOUT":         "",
		"HTTP_IDLE_TIMEOUT":          "",
		"DATABASE_URL":               "postgres://localhost/tripgo",
		"DATABASE_MAX_CONNS":         "10",
		"DATABASE_MIN_CONNS":         "2",
		"DATABASE_MAX_CONN_LIFETIME": "30m",
		"DATABASE_CONNECT_TIMEOUT":   "5s",
		"DATABASE_QUERY_TIMEOUT":     "3s",
		checkIntervalEnv:             "",
		staleAfterEnv:                "",
		workersEnv:                   "",
		queueSizeEnv:                 "",
		pushURLEnv:                   "http://localhost:23809",
		pushTimeoutEnv:               "",
	}
	for name, value := range values {
		t.Setenv(name, value)
	}
}

func TestLoadPositionDefaults(t *testing.T) {
	setTestEnvironment(t)

	for _, name := range []string{
		checkIntervalEnv, staleAfterEnv, workersEnv, queueSizeEnv, pushTimeoutEnv,
	} {
		if err := os.Unsetenv(name); err != nil {
			t.Fatalf("unset %s: %v", name, err)
		}
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	wantPositions := config.PositionsConfig{
		CheckInterval: 30 * time.Second,
		StaleAfter:    60 * time.Second,
		Workers:       4,
		QueueSize:     100,
	}
	if cfg.Positions != wantPositions {
		t.Fatalf("Positions = %+v, want %+v", cfg.Positions, wantPositions)
	}
	if cfg.Push.RequestTimeout != time.Second {
		t.Fatalf("RequestTimeout = %v, want 1s", cfg.Push.RequestTimeout)
	}
	if cfg.Push.HTTPURL != "http://localhost:23809" {
		t.Fatalf("HTTPURL = %q, want configured endpoint", cfg.Push.HTTPURL)
	}
}

func TestLoadPositionOverrides(t *testing.T) {
	setTestEnvironment(t)
	t.Setenv(checkIntervalEnv, "45s")
	t.Setenv(staleAfterEnv, "2m")
	t.Setenv(workersEnv, "8")
	t.Setenv(queueSizeEnv, "256")
	t.Setenv(pushURLEnv, "https://push.example.com/api")
	t.Setenv(pushTimeoutEnv, "750ms")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	wantPositions := config.PositionsConfig{
		CheckInterval: 45 * time.Second,
		StaleAfter:    2 * time.Minute,
		Workers:       8,
		QueueSize:     256,
	}
	if cfg.Positions != wantPositions {
		t.Fatalf("Positions = %+v, want %+v", cfg.Positions, wantPositions)
	}
	wantPush := config.PushConfig{
		HTTPURL:        "https://push.example.com/api",
		RequestTimeout: 750 * time.Millisecond,
	}
	if cfg.Push != wantPush {
		t.Fatalf("Push = %+v, want %+v", cfg.Push, wantPush)
	}
}

func TestLoadRejectsInvalidPositionSettings(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    string
		prefix   string
		wantName string
	}{
		{name: "zero interval", key: checkIntervalEnv, value: "0s", prefix: validationErrorPrefix},
		{name: "negative interval", key: checkIntervalEnv, value: negativeDuration, prefix: validationErrorPrefix},
		{name: "zero stale threshold", key: staleAfterEnv, value: "0s", prefix: validationErrorPrefix},
		{name: "negative stale threshold", key: staleAfterEnv, value: negativeDuration, prefix: validationErrorPrefix},
		{name: "zero workers", key: workersEnv, value: "0", prefix: validationErrorPrefix},
		{name: "negative workers", key: workersEnv, value: "-1", prefix: validationErrorPrefix},
		{name: "zero queue size", key: queueSizeEnv, value: "0", prefix: validationErrorPrefix},
		{name: "negative queue size", key: queueSizeEnv, value: "-1", prefix: validationErrorPrefix},
		{name: "zero push timeout", key: pushTimeoutEnv, value: "0s", prefix: validationErrorPrefix},
		{name: "negative push timeout", key: pushTimeoutEnv, value: negativeDuration, prefix: validationErrorPrefix},
		{name: "duration without unit", key: checkIntervalEnv, value: "30", prefix: parseErrorPrefix, wantName: "CheckInterval"},
		{name: "invalid stale duration", key: staleAfterEnv, value: "soon", prefix: parseErrorPrefix, wantName: "StaleAfter"},
		{name: "invalid workers", key: workersEnv, value: "four", prefix: parseErrorPrefix, wantName: "Workers"},
		{name: "fractional queue size", key: queueSizeEnv, value: "1.5", prefix: parseErrorPrefix, wantName: "QueueSize"},
		{name: "invalid push timeout", key: pushTimeoutEnv, value: "soon", prefix: parseErrorPrefix, wantName: "RequestTimeout"},
		{name: "empty URL", key: pushURLEnv, value: "", prefix: parseErrorPrefix},
		{name: "relative URL", key: pushURLEnv, value: "localhost:23809", prefix: validationErrorPrefix},
		{name: "unsupported URL scheme", key: pushURLEnv, value: "ftp://localhost", prefix: validationErrorPrefix},
		{name: "URL without hostname", key: pushURLEnv, value: "http://:23809", prefix: validationErrorPrefix},
		{name: "malformed URL", key: pushURLEnv, value: "http://[::1", prefix: validationErrorPrefix},
		{name: "zero URL port", key: pushURLEnv, value: "http://localhost:0", prefix: validationErrorPrefix},
		{name: "URL port above limit", key: pushURLEnv, value: "http://localhost:65536", prefix: validationErrorPrefix},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setTestEnvironment(t)
			t.Setenv(tt.key, tt.value)

			_, err := config.Load()
			if err == nil {
				t.Fatal("Load accepted invalid configuration")
			}
			wantName := tt.wantName
			if wantName == "" {
				wantName = tt.key
			}
			if !strings.HasPrefix(err.Error(), tt.prefix) || !strings.Contains(err.Error(), wantName) {
				t.Fatalf("error = %q, want prefix %q and setting %s", err, tt.prefix, wantName)
			}
		})
	}
}

func TestLoadRequiresPushURL(t *testing.T) {
	setTestEnvironment(t)
	if err := os.Unsetenv(pushURLEnv); err != nil {
		t.Fatalf("unset PUSH_HTTP_URL: %v", err)
	}

	_, err := config.Load()
	if err == nil || !strings.Contains(err.Error(), pushURLEnv) {
		t.Fatalf("Load error = %v, want missing PUSH_HTTP_URL", err)
	}
}
