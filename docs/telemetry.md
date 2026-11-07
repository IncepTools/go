# OpenTelemetry

The framework instruments execution boundaries with OpenTelemetry. HTTP
requests, scheduler runs, queue attempts, event handlers, and arbitrary
functions all use the same tracer provider, meter provider, resource, and
propagator.

The framework does not choose a vendor or exporter. OTLP, stdout, Jaeger,
Prometheus bridges, custom exporters, and multiple pipelines can be plugged in
through the OpenTelemetry SDK without changing application instrumentation.

## Configure it once

The lightest production setup uses standard OTLP environment variables. The
same configuration works with SigNoz, an OpenTelemetry Collector, Elastic APM,
Jaeger, Grafana, Honeycomb, and other OTLP-compatible services:

```bash
export OTEL_SERVICE_NAME=catalog
export OTEL_EXPORTER_OTLP_ENDPOINT=https://your-otlp-endpoint:443
export OTEL_EXPORTER_OTLP_HEADERS='api-key=your-key'
```

For a local collector, use `OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318`
and `OTEL_EXPORTER_OTLP_INSECURE=true`. The framework enables OTLP/HTTP only
when an endpoint is present, so simply enabling instrumentation never creates
background connections to localhost.

```go
telemetry, err := telemetry.New(telemetry.Config{
	ServiceName:    "catalog",
	ServiceVersion: version,
})
if err != nil {
	log.Fatal(err)
}

app := inceptools.New(inceptools.Options{
	Service:   "catalog",
	Telemetry: telemetry,
})
```

The `OTLP` field is available when an application prefers code configuration:

```go
telemetry, err := telemetry.New(telemetry.Config{
	ServiceName: "catalog",
	OTLP: telemetry.OTLPConfig{
		Endpoint: "https://collector.example.com:443",
		Headers:  map[string]string{"api-key": os.Getenv("OTEL_API_KEY")},
	},
})
```

`SpanExporter` and `MetricReader` remain optional SDK extension points and take
precedence over the built-in OTLP exporters. For advanced setups, supply
`TracerProvider`, `MeterProvider`, and `Propagator` directly. Providers created
by `telemetry.New` are flushed and shut down by the application lifecycle after
workers and modules have drained.

The standard signal-specific variables are supported too:
`OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`,
`OTEL_EXPORTER_OTLP_METRICS_ENDPOINT`, and their corresponding headers. Resource
attributes can be supplied with `OTEL_RESOURCE_ATTRIBUTES`; `ServiceName` and
`ServiceVersion` in code take precedence for those two fields.

If no exporter or reader is supplied, the SDK providers still make the
instrumentation safe and testable, but telemetry is not delivered to a
backend. Production services should configure an exporter explicitly.

## Automatic boundaries

The application factory adds a server span and request metrics when
`Options.Telemetry` is set. The following framework components also create
spans automatically:

| Boundary | Span | Important attributes |
|---|---|---|
| HTTP | `GET /catalog/products` | method, route, status |
| Scheduler | `scheduler.job` | job name, singleton |
| Queue | `queue.process` | queue, message id, attempt |
| Event bus | `event.handler` | event type, handler, mode |

Each boundary records returned errors and recovered panics. A normal shutdown
cancellation is not reported as an application failure.

## Any function

Use the same helper for work that is not owned by one of the built-in
boundaries:

```go
err := telemetry.Run(ctx, "billing.reconcile",
	func(ctx context.Context) error {
		return reconcile(ctx)
	}, attribute.String("account.id", accountID.String()))
```

For a longer operation where the child context is needed:

```go
ctx, span := telemetry.Start(ctx, "vendor.send")
defer span.End()
if err := client.Send(ctx, payload); err != nil {
	telemetry.RecordError(span, err)
	return err
}
```

## Propagation

The default propagator is W3C Trace Context. Incoming `traceparent` and
`tracestate` headers are extracted before the HTTP server span starts. Use the
OpenTelemetry propagator in outbound clients so a trace continues across
services. The existing `X-Request-Id` remains available for log correlation;
it is not a substitute for a span context.

For any text-map transport:

```go
telemetry.Inject(ctx, propagation.HeaderCarrier(req.Header))
```

## Design rules

- Instrument boundaries once. Business code should add spans only for useful
  operations such as database calls, vendor calls, and expensive decisions.
- Never put user IDs, message bodies, tokens, or unbounded request values into
  metric attributes.
- Use `ctxkit.Detach` before launching work that must outlive an HTTP request;
  events already do this for asynchronous handlers.
- Keep durable work in the outbox or an external broker. OpenTelemetry records
  execution; it does not provide delivery guarantees.
