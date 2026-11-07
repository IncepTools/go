package telemetry_test

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gofiber/fiber/v3"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/IncepTools/go/src/core/errs"
	"github.com/IncepTools/go/src/core/telemetry"
)

// harness builds a Telemetry backed by in-memory providers, so a test can
// assert on what was exported without a collector anywhere.
func harness(t *testing.T) (*telemetry.Telemetry, *tracetest.SpanRecorder, *sdkmetric.ManualReader) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	reader := sdkmetric.NewManualReader()
	tel, err := telemetry.New(telemetry.Config{
		ServiceName:    "test-service",
		TracerProvider: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)),
		MeterProvider:  sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tel.OnShutdown(context.Background()) })
	return tel, recorder, reader
}

func attrOf(t *testing.T, span sdktrace.ReadOnlySpan, key string) attribute.Value {
	t.Helper()
	for _, kv := range span.Attributes() {
		if string(kv.Key) == key {
			return kv.Value
		}
	}
	t.Fatalf("span has no attribute %q", key)
	return attribute.Value{}
}

func TestMiddlewareNamesTheSpanAfterTheRouteTemplate(t *testing.T) {
	tel, recorder, _ := harness(t)
	app := fiber.New()
	app.Use(tel.Middleware())
	app.Get("/users/:id", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })

	for _, id := range []string{"alice", "bob"} {
		resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/users/"+id, nil))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}

	spans := recorder.Ended()
	if len(spans) != 2 {
		t.Fatalf("ended spans = %d, want 2", len(spans))
	}
	// The template, not the URL. Naming a span after the concrete path makes
	// every user id its own operation in the trace backend.
	for _, span := range spans {
		if got := span.Name(); got != "GET /users/:id" {
			t.Errorf("span name = %q, want the route template", got)
		}
		if got := attrOf(t, span, "http.route").AsString(); got != "/users/:id" {
			t.Errorf("http.route = %q", got)
		}
		if got := attrOf(t, span, "http.response.status_code").AsInt64(); got != fiber.StatusOK {
			t.Errorf("status attribute = %d", got)
		}
		if span.SpanKind() != 2 { // trace.SpanKindServer
			t.Errorf("span kind = %v, want server", span.SpanKind())
		}
	}
}

func TestMiddlewareContinuesAnInboundTrace(t *testing.T) {
	tel, recorder, _ := harness(t)
	app := fiber.New()
	app.Use(tel.Middleware())
	app.Get("/", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusNoContent) })

	req := httptest.NewRequest(fiber.MethodGet, "/", nil)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d", len(spans))
	}
	// Without this the gateway's trace and the service's trace are two
	// unrelated traces, which is the whole failure propagation exists to stop.
	if got := spans[0].SpanContext().TraceID().String(); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("trace id = %q, want the caller's", got)
	}
	if got := spans[0].Parent().SpanID().String(); got != "00f067aa0ba902b7" {
		t.Fatalf("parent span id = %q", got)
	}
}

func TestMiddlewareMarksServerFailuresButNotClientErrors(t *testing.T) {
	cases := []struct {
		name       string
		wantStatus int64
		handler    fiber.Handler
		wantError  bool
	}{
		{
			name:       "a 404 is the caller's problem",
			wantStatus: fiber.StatusNotFound,
			handler:    func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusNotFound) },
			wantError:  false,
		},
		{
			name:       "a 500 written by the handler is ours",
			wantStatus: fiber.StatusInternalServerError,
			handler:    func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusInternalServerError) },
			wantError:  true,
		},
		{
			// The regression guard: the exception filter has not run when
			// the middleware unwinds, so reading the response status here
			// reports 200 for a request that will be answered with a 500.
			name:       "a returned error carries its own status",
			wantStatus: fiber.StatusInternalServerError,
			handler:    func(fiber.Ctx) error { return fiber.ErrInternalServerError },
			wantError:  true,
		},
		{
			name:       "a returned typed error carries its mapped status",
			wantStatus: fiber.StatusNotFound,
			handler:    func(fiber.Ctx) error { return errs.NotFound("no such order") },
			wantError:  false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tel, recorder, _ := harness(t)
			app := fiber.New()
			app.Use(tel.Middleware())
			app.Get("/", tc.handler)

			resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/", nil))
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()

			spans := recorder.Ended()
			if len(spans) != 1 {
				t.Fatalf("ended spans = %d", len(spans))
			}
			gotError := spans[0].Status().Code == codes.Error
			if gotError != tc.wantError {
				t.Errorf("span error = %v, want %v (status %v)", gotError, tc.wantError, spans[0].Status())
			}
			if got := attrOf(t, spans[0], "http.response.status_code").AsInt64(); got != tc.wantStatus {
				t.Errorf("http.response.status_code = %d, want %d", got, tc.wantStatus)
			}
		})
	}
}

func TestMiddlewareRecordsRequestCountAndDuration(t *testing.T) {
	tel, _, reader := harness(t)
	app := fiber.New()
	app.Use(tel.Middleware())
	app.Get("/orders/:id", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })

	for i := 0; i < 3; i++ {
		resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/orders/7", nil))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}

	var sawRequests, sawDuration bool
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch m.Name {
			case "inceptools.http.requests":
				sum, ok := m.Data.(metricdata.Sum[int64])
				if !ok || len(sum.DataPoints) != 1 {
					t.Fatalf("unexpected requests data: %#v", m.Data)
				}
				if sum.DataPoints[0].Value != 3 {
					t.Errorf("request count = %d, want 3", sum.DataPoints[0].Value)
				}
				// The route label must be the template, or each id becomes
				// its own time series.
				if route, ok := sum.DataPoints[0].Attributes.Value("http.route"); !ok || route.AsString() != "/orders/:id" {
					t.Errorf("http.route label = %v", route)
				}
				sawRequests = true
			case "inceptools.http.duration":
				hist, ok := m.Data.(metricdata.Histogram[float64])
				if !ok || len(hist.DataPoints) != 1 {
					t.Fatalf("unexpected duration data: %#v", m.Data)
				}
				if hist.DataPoints[0].Count != 3 {
					t.Errorf("duration count = %d, want 3", hist.DataPoints[0].Count)
				}
				sawDuration = true
			}
		}
	}
	if !sawRequests || !sawDuration {
		t.Fatalf("requests=%v duration=%v, want both exported", sawRequests, sawDuration)
	}
}

func TestInjectAndExtractRoundTripAcrossAProcessBoundary(t *testing.T) {
	tel, _, _ := harness(t)
	_ = tel

	ctx, span := telemetry.Start(context.Background(), "caller")
	defer span.End()

	carrier := propagation.MapCarrier{}
	telemetry.Inject(ctx, carrier)
	if carrier["traceparent"] == "" {
		t.Fatal("Inject wrote no traceparent")
	}

	// A different process would start from a bare context and only have the
	// carrier to go on.
	extracted := telemetry.Extract(context.Background(), carrier)
	_, child := telemetry.Start(extracted, "callee")
	defer child.End()

	want := span.SpanContext().TraceID()
	if got := child.SpanContext().TraceID(); got != want {
		t.Fatalf("trace id across the boundary = %s, want %s", got, want)
	}
}

// recordingPropagator exercises the whole TextMapCarrier contract, which the
// default W3C propagator does not: it only ever calls Get on the way in.
type recordingPropagator struct {
	mu   sync.Mutex
	keys []string
	got  map[string]string
}

func (p *recordingPropagator) Inject(_ context.Context, carrier propagation.TextMapCarrier) {
	carrier.Set("x-injected", "yes")
}

func (p *recordingPropagator) Extract(ctx context.Context, carrier propagation.TextMapCarrier) context.Context {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.keys = carrier.Keys()
	p.got = make(map[string]string, len(p.keys))
	for _, key := range p.keys {
		p.got[key] = carrier.Get(key)
	}
	return ctx
}

func (p *recordingPropagator) Fields() []string { return []string{"x-injected"} }

func TestTheRequestCarrierSatisfiesTheFullPropagatorContract(t *testing.T) {
	prop := &recordingPropagator{}
	recorder := tracetest.NewSpanRecorder()
	tel, err := telemetry.New(telemetry.Config{
		ServiceName:    "carrier",
		TracerProvider: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)),
		MeterProvider:  sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewManualReader())),
		Propagator:     prop,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer tel.OnShutdown(context.Background())

	app := fiber.New()
	app.Use(tel.Middleware())
	app.Get("/", func(c fiber.Ctx) error {
		// Injecting onto the response proves Set reaches the real headers.
		telemetry.Inject(c.Context(), fiberResponseCarrier{c})
		return c.SendStatus(fiber.StatusNoContent)
	})

	req := httptest.NewRequest(fiber.MethodGet, "/", nil)
	req.Header.Set("baggage", "tenant=acme")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	prop.mu.Lock()
	defer prop.mu.Unlock()
	if len(prop.keys) == 0 {
		t.Fatal("the carrier reported no keys, so a propagator that enumerates finds nothing")
	}
	if prop.got["baggage"] != "tenant=acme" {
		t.Errorf("baggage read back as %q", prop.got["baggage"])
	}
	if got := resp.Header.Get("x-injected"); got != "yes" {
		t.Errorf("Inject did not reach the response headers: %q", got)
	}
}

// fiberResponseCarrier is the outbound half, so the test can assert that a
// Set on a Fiber context lands on the wire.
type fiberResponseCarrier struct{ c fiber.Ctx }

func (f fiberResponseCarrier) Get(key string) string { return f.c.Get(key) }
func (f fiberResponseCarrier) Set(key, value string) { f.c.Set(key, value) }
func (f fiberResponseCarrier) Keys() []string        { return nil }
