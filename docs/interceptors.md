# Interceptors

An interceptor wraps a handler on both sides: before it runs, after it
returns, and around a failure.

```go
type Interceptor interface {
	Name() string
	Intercept(c fiber.Ctx, next fiber.Handler) error
}
```

A [guard](guards.md) answers yes or no. An interceptor observes and may change
what happens — timing, a response header, a cache lookup, an audit record
written only when the write succeeded.

## Using them

```go
router.Post("/", c.create).
	Guard(guard.Tenanted).
	Intercept(interceptor.OnSuccess("audit", recordCreate)),
```

Per module via `module.WithInterceptors(...)`, per application via
`inceptools.Options.Interceptors`.

Composition is **outside in**: the first listed sees the request first and the
response last.

```
interceptors[0]  ┌─────────────────────────────┐
interceptors[1]  │  ┌───────────────────────┐  │
handler          │  │  ┌─────────────────┐  │  │
                 │  │  └─────────────────┘  │  │
                 │  └───────────────────────┘  │
                 └─────────────────────────────┘
```

## Built in

```go
interceptor.Timing(500 * time.Millisecond)   // warn on anything slower
interceptor.Header("Cache-Control", "no-store")
interceptor.After("audit", func(c fiber.Ctx, err error) { ... })
interceptor.OnSuccess("audit", func(c fiber.Ctx) { ... })
```

`Timing` logs only the slow ones. A line per request is a metric wearing a
log's clothing; a line for the few that were slow gets read.

`After` ignores `fn`'s own error on purpose: an audit write that fails must
not turn a successful request into a 500 the client will retry.

## Writing one

```go
var ETag = interceptor.Of("etag", func(c fiber.Ctx, next fiber.Handler) error {
	if err := next(c); err != nil {
		return err
	}
	c.Set("ETag", hashOf(c.Response().Body()))
	return nil
})
```

With a dependency:

```go
type AuditInterceptor struct{ log *service.Audit }

func (a AuditInterceptor) Name() string { return "audit" }

func (a AuditInterceptor) Intercept(c fiber.Ctx, next fiber.Handler) error {
	err := next(c)
	if err == nil && c.Response().StatusCode() < 400 {
		// Detached, so a client that hung up does not cancel the audit.
		go a.log.Record(ctxkit.Detach(c.Context()), c.Method(), c.Path())
	}
	return err
}
```

## The rule

**Call `next` exactly once on the paths where the handler should run, and
return its error** unless you are deliberately replacing the outcome.

Swallowing that error turns a failed write into a 200. It is the single way to
get this wrong, and it is silent.

```go
// Wrong — the handler failed and the client is told it succeeded.
func (b bad) Intercept(c fiber.Ctx, next fiber.Handler) error {
	_ = next(c)
	return nil
}
```

## Short-circuiting

An interceptor may skip the handler entirely — a cache hit, for instance:

```go
var CacheRead = interceptor.Of("cache", func(c fiber.Ctx, next fiber.Handler) error {
	if body, ok := cache.Get[[]byte](c.Context(), store, key(c)); ok {
		c.Set("X-Cache", "HIT")
		return c.Send(body)
	}
	return next(c)
})
```

Be careful that what you cache is scoped the way the data is. A response
cached without the account in the key is one customer's data served to
another — the same failure the repository's scoping exists to prevent, moved
one layer up. See [Caching](caching.md).

## Interceptor or middleware?

| Use middleware when | Use an interceptor when |
|---|---|
| It applies to the whole application | It applies to one module or one route |
| It is infrastructure: request id, recover, CORS | It is behaviour: audit, caching, retries |
| It never needs to be visible in the route table | You want it recorded with the route |

## Next

- [Guards](guards.md) — before the handler, yes or no.
- [Caching](caching.md) — the store behind a caching interceptor.
