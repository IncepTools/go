# Security

What the framework does for you, and what it still needs from you.

## Account scoping

The single most valuable thing here. A scoped repository applies the
ownership filter itself and **refuses** a query with no account on the
context:

```go
repo.New[entity.Product](db, repo.WithEntityName("product"), repo.Tenanted())
```

Hand-written scoping means every query has to remember
`.Where("account_id = ?", id)`, and the one that forgets does not fail — it
returns another customer's rows, silently, until somebody notices.

Rules worth keeping:

- **A zero account is an error, never "all accounts".**
- **Create takes the account from the context, not the payload.** Otherwise a
  caller writes a row under somebody else's account by setting a field.
- **Another account's row is `NotFound`, not `Forbidden`.** Forbidden
  confirms the id exists to somebody who should not know.
- **`Unscoped` is deliberate and visible.** Reports and reconcilers need it;
  request paths do not.
- Add the account to **every cache key** for per-account data. A response
  cached on the path alone is the same leak one layer up.

## Authorization

[Guards](guards.md) run per route and are recorded in the route table, so
this test is possible:

```go
func TestEveryWriteIsGuarded(t *testing.T) {
	if un := app.Routes.Unguarded(); len(un) > 0 {
		t.Fatalf("mutating routes with no guard: %v", un)
	}
}
```

It is the cheapest security control in the framework. The failure it catches
— a new write endpoint shipped without authentication — is both easy to
commit and expensive to discover.

Guards attach **per route**, never with `Use`, because a `Use` on a group
applies to everything registered after it: add a public route below a
protected one and it is silently exposed.

## Authentication

The framework does **not** authenticate. You supply:

```go
middleware.ChainOptions{Identity: resolveIdentity}
```

Your resolver verifies a signed token and returns the account and actor.
Everything below reads them from the context.

**Never trust a header for identity.** The bundled example does, and says so
in a comment, because an example must run with nothing installed.

Identity establishes *who*; a guard decides *whether*. Keeping them apart is
what lets an endpoint be public and still know who the caller is.

## Not leaking internals

Two mechanisms:

**One response funnel.** Every reply passes through `response.Send`, so the
check for infrastructure detail runs once rather than at four hundred call
sites:

```go
response.SetSanitizer(func(c fiber.Ctx, scope, route, message string) string {
	if looksLikeInfrastructure(message) {
		logx.C(c.Context()).Warn("suppressed an outgoing message", "route", route)
		return "an internal error occurred"
	}
	return message
})
```

**Unclassified errors become generic 500s.** An error that is not an
`*errs.Error` is by definition one nobody checked for leaks, so its message
is replaced and the original goes to the log.

Put anything sensitive in `Cause`, never in `Message`:

```go
errs.Internal("could not reach the pricing service").
	WithCause(err).                 // logged, never sent
	WithMeta("host", upstreamHost)  // logged, never sent
```

## SQL injection

The repository parameterises everything. The one place a string is
interpolated is `ORDER BY`, and it is safe **only** because
[pagination](pagination.md) checks the sort key against an allow-list your
code owns:

```go
var Sortable = []string{"created_at", "name", "price"}
```

With no allow-list configured, sorting is refused. Never build an `ORDER BY`
from a raw parameter.

## Headers

```go
middleware.SecureHeadersOptions{
	HSTSMaxAge:            365 * 24 * time.Hour,
	ContentSecurityPolicy: "default-src 'none'; frame-ancestors 'none'",
	FrameOptions:          "DENY",
}
```

Sets `X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`, a CSP,
`Cross-Origin-Opener-Policy`, `Cross-Origin-Resource-Policy` and
`Permissions-Policy`.

Leave `HSTSMaxAge` at zero in development, or a developer's browser pins
`localhost` to HTTPS and they lose an afternoon.

## CORS

```go
&middleware.CORSOptions{
	AllowOrigins:     []string{"https://app.example.com"},
	AllowCredentials: true,
}
```

Lists origins explicitly. `"*"` is honoured **only** without credentials —
the combination is forbidden by the specification and browsers reject it, so
the framework refuses to emit a configuration that silently never works.

The matched origin is echoed and `Vary: Origin` is always set, so a shared
cache cannot serve one tenant's header to another's browser.

## Rate limiting

```go
&middleware.RateLimitOptions{
	Max: 600, Window: time.Minute,
	KeyFunc: func(c fiber.Ctx) string { return accountKey(c) },
	Store:   redisStore,
}
```

Key on the **account or API key**, not the IP. Keyed on IP, one customer
behind a NAT exhausts the budget for everyone behind it — and an attacker
with a address pool is barely slowed.

The in-process store means N replicas allow N times the limit. Use Redis
where the limit must be the limit.

It fails **open** by default: a store outage should not take the API down.
Set `FailOpen: &false` where the limit protects something that must not be
exceeded.

## Credentials

- `core/config` loads them from the environment, and `secret:"true"` masks
  them in `Describe`.
- `icpt.json` names the **variable**, never the value. A connection string
  holds a password and a repository is the wrong place for one.
- Never log a token, a full DSN or a whole request body.

## Request ids

The middleware **trusts** an inbound `X-Request-Id` so a trace spans
services. Strip it at your public edge, or an attacker chooses your log
correlation ids and can inject into log output.

## Idempotency

[`Idempotency-Key`](idempotency.md) stops a retried POST acting twice. Scope
the key to the account — a global key space lets one account's key collide
with another's and return them somebody else's response body.

## Input

- Bound every string and slice with `max`. The difference between a
  validation error and a 4MB row.
- Set `fiber.Config.BodyLimit` for a service accepting uploads.
- Validate the shape in the DTO; check the business rule in the service,
  because a job needs the same check.

## A checklist before a public launch

- [ ] `app.Routes.Unguarded()` is empty in a test.
- [ ] A sanitiser is installed.
- [ ] Every per-account repository is `Tenanted()`, and every `Unscoped` call
      is named and reviewed.
- [ ] Cache keys include the account.
- [ ] Rate limiting is keyed on the account, backed by a shared store.
- [ ] CORS lists origins explicitly.
- [ ] HSTS is on in production and off in development.
- [ ] `/health` is behind authentication; `/healthz` and `/readyz` are not.
- [ ] `X-Request-Id` is stripped at the edge.
- [ ] `govulncheck` is in CI.

## Reporting a problem in the framework

See [SECURITY.md](../SECURITY.md). Not a public issue.
