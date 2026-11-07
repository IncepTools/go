# Middleware

`web/middleware` is the request-scoped plumbing every route needs before any
handler runs: identity on the context, a trace id, a recovered panic, a
deadline.

## The standard chain

```go
app := inceptools.New(inceptools.Options{
	Middleware: middleware.ChainOptions{
		Timeout:  15 * time.Second,
		Identity: resolveIdentity,
		AccessLog: middleware.AccessLogOptions{
			SkipPaths:     []string{"/livez", "/healthz", "/readyz"},
			SlowThreshold: time.Second,
		},
		SecureHeaders: &middleware.SecureHeadersOptions{
			HSTSMaxAge: 365 * 24 * time.Hour,
		},
	},
	CORS:      &middleware.CORSOptions{AllowOrigins: []string{"https://app.example.com"}, AllowCredentials: true},
	RateLimit: &middleware.RateLimitOptions{Max: 600, Window: time.Minute},
})
```

`middleware.Chain` returns them in the order that is correct. Use it unless
you know why you are deviating.

When `inceptools.Options.Telemetry` is set, the application adds OpenTelemetry
middleware after this chain. It extracts W3C trace context, starts the server
span, records route/status/duration metrics, records handler errors, and puts
the span context on the request before the route runs.

## Why the order is not a style choice

```
logger on the context
CORS
request id        ─ so everything below logs the same id
recover           ─ so a panic below is caught WITH that id
security headers
access log
identity          ─ account and actor on the context
timeout
rate limit
[route] guards
[route] interceptors
handler
```

- **Request id before recover.** A panic caught above the id generator is
  logged with no correlation, which is the one log line you most want to tie
  to a user's report.
- **Recover before everything that can panic.** A panic that escapes to
  Fiber's own recovery loses the request context and the trace id.
- **Identity before timeout.** Otherwise a slow authentication call is
  reported as a request timeout, and you debug the wrong thing.
- **Rate limit after identity**, so its key function can see who is calling.
  Keyed on IP alone, one customer behind a NAT exhausts the budget for
  everyone behind it.

## The pieces

### RequestID

Establishes the correlation id and puts it on the Fiber locals, the Go
context, and the response as `X-Request-Id` and `X-Trace-Id`.

It **trusts** an inbound `X-Request-Id`, which is what makes a trace span a
gateway, a service and a worker. When a caller sends a valid W3C `traceparent`,
the middleware also uses its trace id for `X-Trace-Id` and the Go context. That
is propagation and correlation, not an exporter-backed span; install an
OpenTelemetry SDK when the service needs sampled spans. Trust these headers
only behind your own edge: a public listener should strip them there, or they
become a log-injection vector.

### Recover

Turns a panic into a 500 in the standard envelope, with the stack in the log
and nothing of it on the wire.

### Timeout

Sets a deadline on the context. It **cannot stop a handler that ignores it** —
Go has no way to kill a goroutine — so the value is entirely in handlers and
drivers that honour `ctx`. Code that performs I/O without a context is the
thing to fix.

### AccessLog

Logs failures and slow requests. Successes are off by default:

```go
middleware.AccessLogOptions{
	SkipPaths:     []string{"/livez", "/healthz", "/readyz", "/metrics"},
	SlowThreshold: time.Second,
	LogSuccess:    false,
}
```

A line per request is a metric wearing a log's clothing. It costs money to
store and nobody reads it; a line for the 1% that were slow is read every
time. Put the probes in `SkipPaths` — a liveness check every two seconds is
43,200 lines a day saying nothing happened.

### Identity

```go
func resolveIdentity(c fiber.Ctx) (ctxkit.Tenant, ctxkit.Actor, error) {
	claims, err := verifyBearer(c.Get("Authorization"))
	if err != nil {
		return ctxkit.Tenant{}, ctxkit.Actor{}, nil   // anonymous
	}
	return ctxkit.Tenant{AccountID: claims.Account, Operator: claims.IsAdmin},
		ctxkit.Actor{UserID: claims.User, DeviceID: claims.Device, Roles: claims.Roles},
		nil
}
```

It establishes *who*; a guard decides *whether*. Returning an error here
refuses the request outright — use that only for a credential that is
malformed rather than merely absent, so a public endpoint still works for an
anonymous caller.

Everything below reads the result through `ctxkit`:

```go
ctxkit.TenantOf(ctx)   // Tenant{AccountID, Operator}
ctxkit.ActorOf(ctx)    // Actor{UserID, DeviceID, Roles}
ctxkit.Trace(ctx)
```

### SecureHeaders

```go
&middleware.SecureHeadersOptions{
	HSTSMaxAge:            365 * 24 * time.Hour,
	ContentSecurityPolicy: "default-src 'none'; frame-ancestors 'none'",
	FrameOptions:          "DENY",
}
```

Leave `HSTSMaxAge` at zero in development. Sending HSTS from a service
reachable over plain HTTP pins `localhost` to HTTPS in the developer's
browser, and they lose an afternoon to it.

### CORS

```go
&middleware.CORSOptions{
	AllowOrigins:     []string{"https://app.example.com"},
	AllowCredentials: true,
	MaxAge:           12 * time.Hour,
}
```

It echoes the matched origin rather than the configured list, and always sets
`Vary: Origin`, so a shared cache cannot serve one tenant's origin header to
another's browser.

`"*"` is honoured **only** when `AllowCredentials` is false. The combination
is forbidden by the specification and browsers reject it, so this refuses to
emit a configuration that would silently never work. For wildcard subdomains,
use `AllowOriginFunc`.

### RateLimit

```go
&middleware.RateLimitOptions{
	Max:     600,
	Window:  time.Minute,
	KeyFunc: func(c fiber.Ctx) string { return accountKey(c) },
	Skip:    func(c fiber.Ctx) bool { return c.Path() == "/livez" || c.Path() == "/healthz" },
	Store:   redisStore,
}
```

Sets the standard `RateLimit-Limit`, `RateLimit-Remaining` and
`RateLimit-Reset` headers so a well-behaved client can back off instead of
hammering.

The default store is in-process, which is correct for one instance and for
development. Behind N replicas the effective limit is N times the configured
one, so back it with Redis where the limit must actually be the limit.

By default it **fails open**: a store outage serves the request rather than
taking the API down with it. Set `FailOpen: &false` where the limit protects
something that must not be exceeded.

## Adding your own

Any `fiber.Handler` works:

```go
app.Fiber.Use(func(c fiber.Ctx) error {
	c.Set("X-Service-Version", version)
	return c.Next()
})
```

For something that should be visible in the route table and scoped to one
module or route, write an [interceptor](interceptors.md) instead.

## Next

- [Guards](guards.md) — authorization, per route.
- [Security](security.md) — the whole surface in one place.
