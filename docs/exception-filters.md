# Errors and exception filters

Three pieces work together: one error type, one response funnel, and a filter
that catches whatever escaped.

## One error type

```go
return errs.Conflict("a product named %q already exists", name).
	WithCode("NAME_TAKEN")
```

`*errs.Error` carries:

| Field | Reaches the client? |
|---|---|
| `Kind` | As the HTTP status |
| `Code` | Yes — `meta.code`, the thing clients branch on |
| `Message` | Yes — the only prose a client sees |
| `Details` | Yes — field-scoped, for forms |
| `Cause` | **No.** Logged only. |
| `Meta` | **No.** Logged only. |

That split is the whole design. A driver message, a hostname, a constraint
name go in `Cause`, where they reach the log and nothing else.

## Kinds

| Constructor | Kind | Status |
|---|---|---|
| `errs.Invalid` | `invalid` | 400 |
| `errs.Unauthorized` | `unauthorized` | 401 |
| `errs.PaymentRequired` | `payment` | 402 |
| `errs.Forbidden` | `forbidden` | 403 |
| `errs.NotFound` | `not_found` | 404 |
| `errs.Conflict` | `conflict` | 409 |
| `errs.Exhausted` | `exhausted` | 429 |
| `errs.Internal` | `internal` | 500 |
| `errs.NotSupported` | `not_supported` | 501 |
| `errs.Unavailable` | `unavailable` | 503 |
| `errs.Timeout` | `timeout` | 504 |

A service returns these without importing `net/http`, which is what lets the
same service be called from a job or a CLI.

### Prefer NotFound over Forbidden

Returning 403 for a resource belonging to someone else confirms it exists to
somebody who should not know. Use `NotFound` for both "no such thing" and
"not yours".

## Building them up

```go
errs.Internal("could not reach the pricing service").
	WithCode("PRICING_UNAVAILABLE").
	WithCause(err).                     // logged, never sent
	WithMeta("productId", id.String())  // logged, never sent
```

Every builder returns a copy, so a package-level sentinel can be specialised
without being mutated.

## Wrapping

```go
if err := s.repo.Create(ctx, p); err != nil {
	return errs.Wrap(errs.KindInternal, "could not store the product", err)
}
```

`Wrap` of a nil error is nil, so `return errs.Wrap(kind, msg, err)` is safe on
a possibly-nil error.

`errs.From(err)` normalises anything: an `*errs.Error` (or something wrapping
one) comes back as itself; everything else becomes `KindInternal` with the
original as the cause.

## Inspecting

```go
errs.Is(err, errs.KindConflict)
errs.KindOf(err)
errs.CodeOf(err) == "NAME_TAKEN"
errors.Is(err, gorm.ErrRecordNotFound)   // the chain is intact
```

These work through `fmt.Errorf("%w")` wrapping.

## The response funnel

Every reply goes through `response.Send`, which is why the leak check lives
there rather than at every call site:

```go
response.SetSanitizer(func(c fiber.Ctx, scope, route, message string) string {
	if looksLikeInfrastructure(message) {
		logx.C(c.Context()).Warn("suppressed an outgoing message", "route", route)
		return "an internal error occurred"
	}
	return message
})
```

Install one at boot in any service whose errors can carry infrastructure
detail — which is all of them.

## Rendering an error

```go
return response.Fail(c, err)
```

That is the whole sad path of a handler. `Fail` works out the status, the
code and the field details from the error. An error that is **not** an
`*errs.Error` becomes a generic 500 with its message replaced — because an
unclassified error is by definition one nobody checked for leaks.

## The filter

```go
app := fiber.New(fiber.Config{
	ErrorHandler: filter.ErrorHandler(filter.Options{}),
})
```

`inceptools.New` installs it for you. It is the last line of the pipeline, and
it guarantees three things:

- **Every failure is logged once**, with its internal cause, under the
  request's trace id — including the ones a handler returned without logging.
- **No unclassified error's text reaches a client.**
- **A 404 for an unrouted path looks like every other response**, so a
  client's error parsing does not special-case the framework's own replies.

It also translates Fiber's own errors — unrouted path, method not allowed,
body too large — and distinguishes a cancelled client (not a server failure)
from a deadline exceeded (a timeout worth logging).

### Logging policy

4xx responses are **not** logged by default. A thousand validation failures a
minute is a client bug, and logging them buries the server bugs. Turn them on
while debugging:

```go
filter.Options{LogClientErrors: true}
```

## Custom filters

For an error type the framework does not know:

```go
filter.Options{
	Filters: []filter.Filter{
		filter.Of("payment-provider", func(c fiber.Ctx, err error) (bool, error) {
			var pe *provider.Error
			if !errors.As(err, &pe) {
				return false, nil          // not mine; try the next one
			}
			return true, response.Fail(c, errs.PaymentRequired(pe.UserMessage()).
				WithCode("CARD_DECLINED").WithCause(pe))
		}),
	},
}
```

Return `false` to pass the error along. The chain always ends in the built-in
behaviour, so a filter never has to handle everything.

## The envelope

Success:

```json
{
  "data": { "id": "9c1e…", "name": "Desk lamp" },
  "meta": {
    "success": true,
    "statusCode": 200,
    "message": "product retrieved",
    "traceId": "0b2f…",
    "requestTime": 12,
    "route": "/api/v1/catalog/products/9c1e…"
  }
}
```

Failure:

```json
{
  "data": null,
  "meta": {
    "success": false,
    "statusCode": 400,
    "code": "VALIDATION_FAILED",
    "message": "validation failed",
    "traceId": "0b2f…",
    "errors": [{ "field": "name", "message": "name is required", "rule": "required" }]
  }
}
```

A list adds `page`, `pageSize`, `total` and `pages`. 204 is the one reply
that is not an envelope, because 204 means exactly that.

One shape means a client written against one endpoint can read every
endpoint. The cost is a wrapper object; the benefit is that no consumer ever
has to ask what shape *this* route returns.

## Next

- [Logging](logging.md) — the other half: what the client did not get.
- [Validation](validation.md) — where field-scoped details come from.
