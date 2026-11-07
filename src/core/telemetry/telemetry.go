// Package telemetry is the framework's OpenTelemetry integration.
//
// The framework instruments execution boundaries, not business code: HTTP
// requests, scheduled jobs, queue attempts, and event handlers. Business code
// can use Start or Run for any function and gets the same provider, propagator,
// resource, error recording, and shutdown behavior.
//
// Exporters remain pluggable. Services may provide any OpenTelemetry SDK
// exporter through Config, or supply their own providers when they need more
// advanced sampling, views, or multiple pipelines.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/IncepTools/go/src/web/response"
)

// InstrumentationName is the stable scope used by framework spans and meters.
const InstrumentationName = "github.com/IncepTools/go"

// Config configures the built-in providers. SpanExporter and MetricReader are
// optional SDK hooks; custom providers take precedence when supplied.
type Config struct {
	ServiceName    string
	ServiceVersion string
	// OTLP enables the standard OTLP/HTTP exporters when an endpoint is
	// configured explicitly or through OTEL_EXPORTER_OTLP_* environment
	// variables. It is deliberately opt-in by endpoint: New never attempts to
	// connect to localhost just because telemetry was enabled.
	OTLP           OTLPConfig
	SpanExporter   sdktrace.SpanExporter
	MetricReader   sdkmetric.Reader
	TracerProvider trace.TracerProvider
	MeterProvider  metric.MeterProvider
	Propagator     propagation.TextMapPropagator
}

// OTLPConfig is the small, vendor-neutral configuration surface shared by
// SigNoz, an OpenTelemetry Collector, Elastic APM, Jaeger, Grafana, Honeycomb,
// and other OTLP-compatible backends. Signal-specific endpoints override the
// common endpoint. Standard OTEL_* environment variables are also honored by
// the exporters.
type OTLPConfig struct {
	Endpoint        string
	TracesEndpoint  string
	MetricsEndpoint string
	Headers         map[string]string
	Insecure        bool
	Timeout         time.Duration
}

// Telemetry owns providers created by New and flushes them during shutdown.
type Telemetry struct {
	tracer trace.Tracer
	meter  metric.Meter
	prop   propagation.TextMapPropagator
	close  []func(context.Context) error

	requests metric.Int64Counter
	duration metric.Float64Histogram
}

// New creates providers, installs them as the process globals, and returns a
// lifecycle-ready telemetry component. The default SDK providers are active
// even without exporters; instrumentation call sites never change when an
// exporter or backend is added.
func New(cfg Config) (*Telemetry, error) {
	serviceName := strings.TrimSpace(cfg.ServiceName)
	if serviceName == "" {
		serviceName = strings.TrimSpace(os.Getenv("OTEL_SERVICE_NAME"))
	}
	if serviceName == "" {
		return nil, errors.New("telemetry: service name is required")
	}
	version := cfg.ServiceVersion
	attrs := resourceAttributes(os.Getenv("OTEL_RESOURCE_ATTRIBUTES"))
	if version == "" {
		version = attrs["service.version"]
	}
	attrs["service.name"] = serviceName
	if version != "" {
		attrs["service.version"] = version
	}
	resourceAttrs := make([]attribute.KeyValue, 0, len(attrs))
	for key, value := range attrs {
		resourceAttrs = append(resourceAttrs, attribute.String(key, value))
	}
	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes("", resourceAttrs...))
	if err != nil {
		return nil, fmt.Errorf("telemetry: resource: %w", err)
	}

	var closeFns []func(context.Context) error
	tp := cfg.TracerProvider
	if tp == nil {
		opts := []sdktrace.TracerProviderOption{sdktrace.WithResource(res)}
		exporter := cfg.SpanExporter
		if exporter == nil && cfg.OTLP.hasTraces() {
			var exportOpts []otlptracehttp.Option
			if endpoint := cfg.OTLP.traceEndpoint(); endpoint != "" {
				exportOpts = append(exportOpts, otlptracehttp.WithEndpointURL(endpoint))
			}
			if len(cfg.OTLP.Headers) > 0 {
				exportOpts = append(exportOpts, otlptracehttp.WithHeaders(cfg.OTLP.Headers))
			}
			if cfg.OTLP.Insecure {
				exportOpts = append(exportOpts, otlptracehttp.WithInsecure())
			}
			if cfg.OTLP.Timeout > 0 {
				exportOpts = append(exportOpts, otlptracehttp.WithTimeout(cfg.OTLP.Timeout))
			}
			exporter, err = otlptracehttp.New(context.Background(), exportOpts...)
			if err != nil {
				return nil, fmt.Errorf("telemetry: OTLP trace exporter: %w", err)
			}
		}
		if exporter != nil {
			opts = append(opts, sdktrace.WithBatcher(exporter))
		}
		provider := sdktrace.NewTracerProvider(opts...)
		tp = provider
		closeFns = append(closeFns, provider.Shutdown)
	}
	mp := cfg.MeterProvider
	if mp == nil {
		opts := []sdkmetric.Option{sdkmetric.WithResource(res)}
		reader := cfg.MetricReader
		if reader == nil && cfg.OTLP.hasMetrics() {
			var exportOpts []otlpmetrichttp.Option
			if endpoint := cfg.OTLP.metricEndpoint(); endpoint != "" {
				exportOpts = append(exportOpts, otlpmetrichttp.WithEndpointURL(endpoint))
			}
			if len(cfg.OTLP.Headers) > 0 {
				exportOpts = append(exportOpts, otlpmetrichttp.WithHeaders(cfg.OTLP.Headers))
			}
			if cfg.OTLP.Insecure {
				exportOpts = append(exportOpts, otlpmetrichttp.WithInsecure())
			}
			if cfg.OTLP.Timeout > 0 {
				exportOpts = append(exportOpts, otlpmetrichttp.WithTimeout(cfg.OTLP.Timeout))
			}
			exporter, exportErr := otlpmetrichttp.New(context.Background(), exportOpts...)
			if exportErr != nil {
				return nil, fmt.Errorf("telemetry: OTLP metric exporter: %w", exportErr)
			}
			reader = sdkmetric.NewPeriodicReader(exporter)
		}
		if reader != nil {
			opts = append(opts, sdkmetric.WithReader(reader))
		}
		provider := sdkmetric.NewMeterProvider(opts...)
		mp = provider
		closeFns = append(closeFns, provider.Shutdown)
	}
	prop := cfg.Propagator
	if prop == nil {
		prop = propagation.TraceContext{}
	}
	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	otel.SetTextMapPropagator(prop)

	t := &Telemetry{
		tracer: tp.Tracer(InstrumentationName),
		meter:  mp.Meter(InstrumentationName),
		prop:   prop,
		close:  closeFns,
	}
	var instrumentErr error
	t.requests, instrumentErr = t.meter.Int64Counter("inceptools.http.requests", metric.WithDescription("HTTP requests"), metric.WithUnit("{request}"))
	if instrumentErr != nil {
		return nil, instrumentErr
	}
	t.duration, instrumentErr = t.meter.Float64Histogram("inceptools.http.duration", metric.WithDescription("HTTP request duration"), metric.WithUnit("s"))
	if instrumentErr != nil {
		return nil, instrumentErr
	}
	return t, nil
}

func (c OTLPConfig) hasTraces() bool {
	return c.traceEndpoint() != "" || os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" || os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") != ""
}

func (c OTLPConfig) hasMetrics() bool {
	return c.metricEndpoint() != "" || os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" || os.Getenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT") != ""
}

func (c OTLPConfig) traceEndpoint() string {
	if c.TracesEndpoint != "" {
		return c.TracesEndpoint
	}
	if c.Endpoint != "" && os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") == "" {
		return strings.TrimRight(c.Endpoint, "/") + "/v1/traces"
	}
	return ""
}

func (c OTLPConfig) metricEndpoint() string {
	if c.MetricsEndpoint != "" {
		return c.MetricsEndpoint
	}
	if c.Endpoint != "" && os.Getenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT") == "" {
		return strings.TrimRight(c.Endpoint, "/") + "/v1/metrics"
	}
	return ""
}

func resourceAttributes(raw string) map[string]string {
	attrs := make(map[string]string)
	for _, item := range strings.Split(raw, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(item), "=")
		if ok && strings.TrimSpace(key) != "" {
			attrs[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return attrs
}

// Start starts a span in the framework instrumentation scope.
func Start(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return otel.Tracer(InstrumentationName).Start(ctx, name, trace.WithAttributes(attrs...))
}

// Inject writes the current span context into an outbound carrier. Use it for
// HTTP, messaging, or any other transport that supports a text map.
func Inject(ctx context.Context, carrier propagation.TextMapCarrier) {
	otel.GetTextMapPropagator().Inject(ctx, carrier)
}

// Extract reads a propagated span context from an inbound carrier.
func Extract(ctx context.Context, carrier propagation.TextMapCarrier) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}

// Run traces one arbitrary execution and records a returned error on the span.
func Run(ctx context.Context, name string, fn func(context.Context) error, attrs ...attribute.KeyValue) error {
	ctx, span := Start(ctx, name, attrs...)
	defer span.End()
	err := fn(ctx)
	RecordError(span, err)
	return err
}

// RecordError applies the OpenTelemetry error convention. Cancellation during
// a normal shutdown is not marked as an application failure.
func RecordError(span trace.Span, err error) {
	if err == nil || span == nil || errors.Is(err, context.Canceled) {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

// OnShutdown flushes and closes providers created by New.
func (t *Telemetry) OnShutdown(ctx context.Context) error {
	var errs []error
	for i := len(t.close) - 1; i >= 0; i-- {
		if err := t.close[i](ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Middleware creates automatic HTTP spans, context extraction, and standard
// request metrics. It is safe to install alongside the framework request-ID
// middleware; request IDs remain available to logs while OTel owns trace IDs.
func (t *Telemetry) Middleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		ctx := t.prop.Extract(c.Context(), fiberCarrier{ctx: c})
		start := time.Now()
		ctx, span := t.tracer.Start(ctx, c.Method(), trace.WithSpanKind(trace.SpanKindServer), trace.WithAttributes(
			attribute.String("http.request.method", c.Method()),
		))
		c.SetContext(ctx)
		err := c.Next()
		elapsed := time.Since(start)
		// Take the status from the error when there is one: the exception
		// filter has not run yet, so the response still carries the default,
		// and the span would claim a failed request returned 200.
		status := c.Response().StatusCode()
		if err != nil {
			status = response.StatusForError(err)
		}
		route := c.Path()
		if matched := c.Route(); matched != nil && matched.Path != "" {
			route = matched.Path
		}
		span.SetName(c.Method() + " " + route)
		span.SetAttributes(attribute.String("http.route", route), attribute.Int("http.response.status_code", status))
		// Record the detail of any failure, but set the span STATUS from the
		// resolved HTTP status alone. OpenTelemetry reserves Error on a
		// server span for 5xx, and tying it to `err != nil` instead would
		// make the error rate depend on handler style: a 404 written with
		// SendStatus would be healthy while the same 404 returned as a typed
		// error would not.
		if err != nil && !errors.Is(err, context.Canceled) {
			span.RecordError(err)
		}
		if status >= http.StatusInternalServerError {
			span.SetStatus(codes.Error, fmt.Sprintf("HTTP %d", status))
		}
		attrs := metric.WithAttributes(attribute.String("http.request.method", c.Method()), attribute.String("http.route", route), attribute.Int("http.response.status_code", status))
		t.requests.Add(ctx, 1, attrs)
		t.duration.Record(ctx, elapsed.Seconds(), attrs)
		span.End()
		return err
	}
}

type fiberCarrier struct{ ctx fiber.Ctx }

func (h fiberCarrier) Get(key string) string { return h.ctx.Get(key) }
func (h fiberCarrier) Set(key, value string) { h.ctx.Set(key, value) }
func (h fiberCarrier) Keys() []string        { return []string{"traceparent", "tracestate", "baggage"} }
