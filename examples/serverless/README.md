# serverless

The same application, hosted without `Run`.

```bash
go run ./serverless

curl localhost:8089/api/v1/quotes/quotes/acme      # {"currency":"USD",...}
curl -X POST localhost:8089/api/v1/quotes/quotes/reload
curl localhost:8089/api/v1/quotes/quotes/acme      # {"currency":"EUR",...}
curl localhost:8089/api/v1/quotes/quotes/unknown   # 404, typed
curl localhost:8089/livez
```

`Application` implements `http.Handler`, so it is passed straight to
`http.Server`. A Lambda deployment swaps those four lines for the adapter and
changes nothing else:

```go
adapter := httpadapter.NewV2(app)
lambda.Start(adapter.ProxyWithContext)
```

`net/http` is used here so the example runs with no AWS dependency and no
credentials. Everything else — the descriptor, the middleware chain, the
health routes, the typed 404 — is the same code the other eight examples run.

## The two things that differ from a server

**`Prepare`, not `Run`.** `Run` owns a listener and a signal loop, and a
function runtime has neither. `Prepare` does the rest of what `Run` would do —
eager wiring, init hooks, ready hooks — and is safe to call on every
invocation, because it runs once per cold start. `ServeHTTP` calls it anyway,
so calling it during the cold start is an optimisation: it moves the wiring
cost off the first request instead of charging it to whoever arrives first.

**`Reload`, not `SIGHUP`.** No signals reach a function, so refreshing
configuration has to be an explicit call. `app.OnReload` registers the
callback once; `app.Reload(ctx)` runs it. The same callback is what `SIGHUP`
invokes when the identical application is hosted by `Run`.

The reload is a route here so it is one `curl` away. A real function calls
`app.Reload` from its control-plane lifecycle and does not expose it.

## What the example does not do per invocation

Build the application. It is constructed once, outside the handler, and kept
for the life of the execution environment — a handler that wires it per
invocation pays the cost every time and throws away every pooled client it
opened. The same rule covers telemetry: build one `telemetry.Telemetry`
during the cold start, pass it as `Options.Telemetry`, and let the batch
exporters flush on their own interval.

`Shutdown` is the matching case in the other direction. It flushes telemetry
and closes clients, so it belongs to environment termination, not to the end
of a request.

Next: [`docs/serverless.md`](../../docs/serverless.md) for the hosting modes
in full, and [`docs/telemetry.md`](../../docs/telemetry.md) for the exporter
setup this example leaves as a comment.
