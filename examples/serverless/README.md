# serverless

The same application, hosted without `Run`.

```bash
go run ./serverless

curl localhost:8089/api/v1/catalog/quotes/acme      # "currency":"USD"
curl localhost:8089/api/v1/catalog/quotes/unknown   # 404, typed
curl localhost:8089/livez

# Refresh configuration with no restart:
echo '{"currency":"EUR"}' > quotes.json
curl -X POST localhost:8089/__control/reload
curl localhost:8089/api/v1/catalog/quotes/acme      # "currency":"EUR"

# A bad file is reported and changes nothing:
echo 'not json' > quotes.json
curl -X POST localhost:8089/__control/reload        # 500
curl localhost:8089/api/v1/catalog/quotes/acme      # still EUR
```

## Layout

```
apps/catalog/                   the domain
  catalog_app.go                Descriptor, Providers, NewApp
  quotes/                       the module
    quotes_module.go            Providers, New, OnReload
    constants/                  module name, config path, defaults
    dto/                        Quote
    service/                    pricing, and the reloadable configuration
    controller/                 the HTTP surface
```

`Application` implements `http.Handler`, so it is passed straight to
`http.Server`. A Lambda deployment swaps those lines for the adapter and
changes nothing else:

```go
adapter := httpadapter.NewV2(app)
lambda.Start(adapter.ProxyWithContext)
```

`net/http` is used here so the example runs with no AWS dependency and no
credentials. Everything else — the domain, the middleware chain, the health
routes, the typed 404 — is the same code the other nine examples run.

## The two things that differ from a server

**`Prepare`, not `Run`.** `Run` owns a listener and a signal loop, and a
function runtime has neither. `Prepare` does the rest of what `Run` would do —
eager wiring, init hooks, ready hooks — and is safe to call on every
invocation, because it runs once per cold start. `ServeHTTP` calls it anyway,
so calling it during the cold start is an optimisation: it moves the wiring
cost off the first request instead of charging it to whoever arrives first.

**`Reload`, not `SIGHUP`.** No signals reach a function, so refreshing
configuration has to be an explicit call.

## Who owns the reload

The module does. `quotes` implements `OnReload`, and a mounted module is a
lifecycle participant, so that is the whole registration — `main` never
mentions it. The same method is what `SIGHUP` reaches when this identical
application is hosted by `Run`.

Triggering the reload is the **host's** job, not a route. A Lambda extension,
a parameter-store subscription or a deploy hook calls `app.Reload(ctx)`; the
`/__control/reload` endpoint here stands in for that, and is deliberately
outside the application's own routes so a configuration refresh is not
something any caller can trigger.

Two properties the module's reload keeps, and a real one must: it is safe to
call while requests are in flight — the configuration is swapped as a whole,
so no request sees half of it — and a failure leaves the previous
configuration in place rather than half-applying a new one. One module failing
to reload does not stop the others, and the process keeps serving.

## What the example does not do per invocation

Build the application. It is constructed once, outside the handler, and kept
for the life of the execution environment — a handler that wires it per
invocation pays the cost every time and throws away every pooled client it
opened. The same rule covers telemetry: build one `telemetry.Telemetry` during
the cold start, pass it as `Options.Telemetry`, and let the batch exporters
flush on their own interval.

`Shutdown` is the matching case in the other direction. It flushes telemetry
and closes clients, so it belongs to environment termination, not to the end
of a request.

Next: [`docs/serverless.md`](../../docs/serverless.md) for the hosting modes
in full, and [`docs/telemetry.md`](../../docs/telemetry.md) for the exporter
setup this example leaves as a comment.
