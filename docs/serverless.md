# Server and serverless

The application factory has one configuration and two hosting modes. The
domain modules, middleware, routes, health checks, telemetry, errors, and
reload callbacks do not change between them.

## Long-lived server

```go
if err := app.Run(ctx); err != nil {
	log.Fatal(err)
}
```

`Run` performs eager dependency construction, starts the listener, runs ready
hooks, handles `SIGTERM` draining, flushes telemetry, and invokes shutdown
hooks in reverse order. `SIGHUP` invokes the callbacks registered with
`app.OnReload`.

## Lambda or `net/http` adapter

`Application` implements `http.Handler`:

```go
adapter := httpadapter.NewV2(app)
lambda.Start(func(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	return adapter.ProxyWithContext(ctx, req)
})
```

Build the application once during the cold start and reuse it across
invocations. The first request calls `Application.Prepare` once, which eagerly
builds dependencies and runs init/ready hooks; subsequent warm invocations
reuse the prepared application. The adapter runs the same Fiber pipeline
through the in-process request harness; it does not start a second listener.

Do not call `Run` from a Lambda handler. There is no listener to drain and no
reliable SIGHUP. When configuration changes, call:

```go
if err := app.Reload(ctx); err != nil {
	return err
}
```

## Telemetry

Create one `telemetry.Telemetry` instance during cold start and pass it through
`inceptools.Options.Telemetry`. HTTP spans, event spans, queue attempts,
scheduler runs, and custom `telemetry.Run` calls share the same provider and
propagator. Batch exporters continue to flush on their configured interval;
call `app.Shutdown(ctx)` only when the runtime is actually terminating, not
after each invocation.

## Probes

The standard routes are available through the adapter as well:

- `/livez` — process liveness; checks nothing.
- `/readyz` — dependency readiness; returns 503 when a critical check fails.
- `/health` — detailed topology; protect it when the adapter is public.
- `/healthz` — compatibility alias for `/livez`.

Keep the handler stateless across invocations except for deliberately shared
caches and clients. Request identity, deadlines, trace context, and tenant
scope must come from each invocation's context.

## A runnable version

[`examples/serverless`](../examples/serverless) is this page as working code:
one application hosted through `http.Handler`, with `Prepare` on the cold
start and `Reload` in place of `SIGHUP`. It uses `net/http` rather than Lambda
so it runs with no AWS dependency; the adapter is a four-line swap.
