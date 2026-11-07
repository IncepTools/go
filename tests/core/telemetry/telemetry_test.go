package telemetry_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/IncepTools/go/src/core/telemetry"
)

func TestRunUsesConfiguredExporterAndRecordsFailure(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := trace.NewTracerProvider(trace.WithSpanProcessor(recorder))
	tel, err := telemetry.New(telemetry.Config{
		ServiceName:    "test-service",
		TracerProvider: provider,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer tel.OnShutdown(context.Background())

	want := errors.New("worker failed")
	if err := telemetry.Run(context.Background(), "worker.execute", func(context.Context) error {
		return want
	}); !errors.Is(err, want) {
		t.Fatalf("Run error = %v", err)
	}

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d", len(spans))
	}
	if got := spans[0].Name(); got != "worker.execute" {
		t.Fatalf("span name = %q", got)
	}
	if len(spans[0].Events()) != 1 {
		t.Fatalf("span error events = %d", len(spans[0].Events()))
	}
}

func TestTelemetryCanBeUsedByConcurrentWorkers(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := trace.NewTracerProvider(trace.WithSpanProcessor(recorder))
	tel, err := telemetry.New(telemetry.Config{ServiceName: "test", TracerProvider: provider})
	if err != nil {
		t.Fatal(err)
	}
	defer tel.OnShutdown(context.Background())

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = telemetry.Run(context.Background(), "parallel.work", func(context.Context) error { return nil })
		}()
	}
	wg.Wait()
	if got := len(recorder.Ended()); got != 16 {
		t.Fatalf("ended spans = %d, want 16", got)
	}
}
