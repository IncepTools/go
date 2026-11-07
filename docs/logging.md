# Logging

`core/logx` is `log/slog` with the request-scoped identity attached
automatically.

```go
logger := logx.New(logx.Options{
	Level:   logx.ParseLevel(cfg.LogLevel),   // debug | info | warn | error
	Format:  logx.FormatJSON,                 // or FormatText in development
	Service: "shop",
})
slog.SetDefault(logger)
```

Pass it as `inceptools.Options.Logger` and it is placed on every request
context.

## Logging in a handler or a service

```go
logx.C(ctx).Info("product published", "productId", id.String())
```

`logx.C` returns the context's logger with the request's fields already on it:

```json
{
  "time": "2026-01-14T09:21:04Z",
  "level": "INFO",
  "msg": "product published",
  "service": "shop",
  "traceId": "0b2f…",
  "requestId": "0b2f…",
  "accountId": "1a3c…",
  "userId": "77ff…",
  "productId": "9c1e…"
}
```

Use it at the point of logging rather than building a decorated logger and
passing it around — the fields come from the context, which you already have.

## Logging an error

```go
if err != nil {
	logx.Err(ctx, "could not publish the product", err, "productId", id.String())
	return err
}
```

`logx.Err` logs everything the client did **not** get: the kind, the code, the
internal cause and the structured meta.

```json
{
  "level": "ERROR",
  "msg": "could not publish the product",
  "error": "could not reach the pricing service",
  "kind": "internal",
  "code": "PRICING_UNAVAILABLE",
  "cause": "dial tcp 10.1.4.7:8080: connection refused",
  "productId": "9c1e…",
  "traceId": "0b2f…"
}
```

That pairing is the point. The response funnel sends the user a safe sentence
and a trace id; this sends the operator the whole story under the same id.
Logging only what you returned leaves an incident as a trace id with no
explanation behind it.

### Levels are chosen for you

`logx.Err` picks the level from the error's kind:

| Kind | Level | Why |
|---|---|---|
| invalid, not_found, unauthorized, forbidden, conflict, exhausted, payment | **warn** | Caller-caused. Real, but not a defect — do not page on these. |
| everything else | **error** | A defect or a dependency failure. |

## The output

**JSON in production, text in development.** Set it from configuration rather
than from a build tag, so you can flip it without a rebuild.

**Stdout, not stderr.** `stderr` is for the process dying, and mixing the two
makes a crash hard to find in an aggregator.

**No `AddSource` in hot paths.** It costs a stack walk per record; worth it at
error level, wasteful at debug in a loop.

## What not to log

- **Never a credential, a token or a full DSN.** `config.Describe` masks
  what it is told to; a hand-written line masks nothing.
- **Never a whole request body.** It contains whatever the user typed,
  including the things they should not have.
- **Not a line per successful request.** That is a metric wearing a log's
  clothing. `AccessLog` logs failures and slow requests by default for exactly
  this reason — see [Middleware](middleware.md).

## Background work

A goroutine that outlives the request must keep the trace id and drop the
cancellation:

```go
go func(ctx context.Context) {
	logx.C(ctx).Info("reindex started")   // same traceId as the request
	...
}(ctxkit.Detach(ctx))
```

`ctxkit.Detach` keeps the trace, the account and the actor and removes the
deadline. Passing the request context directly means the work stops the
moment the client disconnects — silently.

## Correlation across services

The request id middleware trusts an inbound `X-Request-Id` and echoes it, so a
trace spans a gateway, a service and a worker. Forward it from any client you
write:

```go
req.Header.Set("X-Request-Id", ctxkit.Trace(ctx))
```

Strip the header at your public edge, or an attacker chooses your log
correlation ids.

## Testing

```go
var buf bytes.Buffer
logger := logx.New(logx.Options{Writer: &buf, Format: logx.FormatJSON})
ctx := logx.Into(context.Background(), logger)
```

## Next

- [Exception filters](exception-filters.md) — the client's half of a failure.
- [Middleware](middleware.md) — access logging and request ids.
