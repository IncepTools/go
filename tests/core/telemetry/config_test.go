package telemetry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/IncepTools/go/src/core/telemetry"
)

func TestNewRequiresAServiceName(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "")
	if _, err := telemetry.New(telemetry.Config{}); err == nil {
		t.Fatal("New accepted an empty service name")
	}
}

func TestNewFallsBackToTheStandardEnvironmentVariables(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "from-env")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "service.version=2.1.0, deployment.environment=staging")

	tel, err := telemetry.New(telemetry.Config{
		TracerProvider: sdktrace.NewTracerProvider(),
		MeterProvider:  sdkmetric.NewMeterProvider(),
	})
	if err != nil {
		t.Fatalf("New with OTEL_SERVICE_NAME set: %v", err)
	}
	if err := tel.OnShutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestNewDoesNotStartAnExporterWithoutAnEndpoint(t *testing.T) {
	// The guarantee: enabling telemetry must not make the process start
	// dialing localhost:4318 on its own. Nothing here configures an endpoint,
	// so New must complete without one and export nowhere.
	for _, key := range []string{
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
		"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
	} {
		t.Setenv(key, "")
	}

	tel, err := telemetry.New(telemetry.Config{ServiceName: "no-exporter"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := tel.OnShutdown(ctx); err != nil {
		t.Fatalf("shutdown with no exporter: %v", err)
	}
}

func TestNewBuildsOTLPExportersFromACommonEndpoint(t *testing.T) {
	for _, key := range []string{
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
		"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
	} {
		t.Setenv(key, "")
	}

	// One endpoint covers both signals: the config derives /v1/traces and
	// /v1/metrics from it, which is what makes SigNoz, a collector and
	// Honeycomb the same two lines of configuration.
	tel, err := telemetry.New(telemetry.Config{
		ServiceName: "otlp",
		OTLP: telemetry.OTLPConfig{
			// Routable but unreachable, so nothing can accidentally export
			// off the machine running the tests.
			Endpoint: "http://127.0.0.1:1",
			Headers:  map[string]string{"signoz-access-token": "secret"},
			Insecure: true,
			Timeout:  100 * time.Millisecond,
		},
	})
	if err != nil {
		t.Fatalf("New with an OTLP endpoint: %v", err)
	}

	// Shutting down must not hang on an endpoint that will never answer.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- tel.OnShutdown(ctx) }()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("OnShutdown hung on an unreachable OTLP endpoint")
	}
}

func TestNewAcceptsSignalSpecificEndpoints(t *testing.T) {
	for _, key := range []string{
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
		"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
	} {
		t.Setenv(key, "")
	}

	tel, err := telemetry.New(telemetry.Config{
		ServiceName: "split-signals",
		OTLP: telemetry.OTLPConfig{
			TracesEndpoint:  "http://127.0.0.1:1/v1/traces",
			MetricsEndpoint: "http://127.0.0.1:1/v1/metrics",
			Timeout:         100 * time.Millisecond,
		},
	})
	if err != nil {
		t.Fatalf("New with split endpoints: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = tel.OnShutdown(ctx)
}

func TestRecordErrorIgnoresCancellation(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tel, err := telemetry.New(telemetry.Config{
		ServiceName:    "cancellation",
		TracerProvider: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)),
		MeterProvider:  sdkmetric.NewMeterProvider(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer tel.OnShutdown(context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// A job stopped by a routine deploy is not an application failure, and
	// marking it as one pages somebody every release.
	runErr := telemetry.Run(ctx, "drained.job", func(ctx context.Context) error {
		return ctx.Err()
	})
	if !errors.Is(runErr, context.Canceled) {
		t.Fatalf("Run error = %v, want context.Canceled", runErr)
	}

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d", len(spans))
	}
	if len(spans[0].Events()) != 0 {
		t.Errorf("cancellation was recorded as a span error event: %#v", spans[0].Events())
	}
	if spans[0].Status().Code.String() == "Error" {
		t.Errorf("cancellation set the span status to Error")
	}
}
