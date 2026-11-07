# Guards

A guard answers one question before the handler runs: **may this caller do
this?**

```go
type Guard interface {
	Name() string
	CanActivate(c fiber.Ctx) error
}
```

Return `nil` to allow. Return an error to refuse — and the error's kind picks
the status, so a guard that needs to answer 402 rather than 403 just says so.

## Using them

Per route, which is the normal case:

```go
router.Post("/", c.create).Guard(guard.Tenanted),
router.Delete("/:id", c.remove).Guard(guard.Tenanted, guard.HasRole("catalog:write")),
```

Per module, for something every route needs:

```go
module.NewBase("products",
	module.WithControllers(ctrl),
	module.WithGuards(guard.Authenticated),
)
```

Per application, in `inceptools.Options.Guards`.

Module and application guards run **before** the route's own, in that order.

## Why not an `if` at the top of the handler

Three reasons, in increasing order of importance:

1. A route is declared in one line, and its guards are part of that line. A
   reviewer reading `Routes()` sees what protects each route without opening
   six handlers.
2. A route added without protection is *visibly* different from its
   neighbours.
3. The route table records it, so a test can assert that no mutating route is
   unguarded. An `if` inside a handler is invisible to that test.

```go
func TestEveryWriteIsGuarded(t *testing.T) {
	if un := app.Routes.Unguarded(); len(un) > 0 {
		t.Fatalf("mutating routes with no guard: %v", un)
	}
}
```

## Why not `r.Use(...)`

A `Use` on a group applies to every route registered after it. Add a public
route below a protected one and it is silently exposed; the diff looks
harmless. Guards attach per route here precisely so that cannot happen.

## Built in

| | |
|---|---|
| `guard.Authenticated` | Refuses a request with no actor. |
| `guard.Tenanted` | Refuses a request not scoped to an account. |
| `guard.HasRole("a", "b")` | Allows a caller holding any of the roles. |
| `guard.Operator` | Allows only a platform operator. |

These read what the identity middleware put on the context; they do not
themselves verify a token. Keeping the two apart means a test can establish an
actor without minting a credential.

`guard.Tenanted` matters more than it looks. A zero account id reaching a
repository is the bug that returns another customer's rows — the repository
refuses it too, but refusing at the edge gives a better message and costs
nothing.

## Writing one

```go
var NotSuspended = guard.Of("not-suspended", func(c fiber.Ctx) error {
	acct, err := accounts.Load(c.Context(), ctxkit.TenantOf(c.Context()).AccountID)
	if err != nil {
		return err
	}
	if acct.Suspended {
		return errs.Forbidden("this account is suspended").
			WithCode("ACCOUNT_SUSPENDED")
	}
	return nil
})
```

A guard with a dependency is a struct:

```go
type QuotaGuard struct{ quotas *service.Quotas }

func (g QuotaGuard) Name() string { return "quota" }

func (g QuotaGuard) CanActivate(c fiber.Ctx) error {
	ok, err := g.quotas.HasHeadroom(c.Context(), "products")
	if err != nil {
		return err
	}
	if !ok {
		return errs.Exhausted("product quota reached").WithCode("QUOTA_EXCEEDED")
	}
	return nil
}
```

## Ordering

Order is yours to choose and it matters: **put the cheap checks first**. An
authentication guard that reads a cached token should run before a quota guard
that queries a database, so an unauthenticated flood costs a map lookup rather
than a connection.

## Combining

```go
guard.All("writer", guard.Tenanted, guard.HasRole("catalog:write"))
guard.Any("owner-or-admin", IsOwner, guard.Operator)
```

`Any` passes when at least one passes, and reports the **last** refusal when
none do. Use it for "an owner or an admin may do this". Do not use it to paper
over a guard that is failing for a reason you have not understood.

## Status codes

The kind of the returned error decides the response:

| Return | Status |
|---|---|
| `errs.Unauthorized(...)` | 401 — who are you |
| `errs.Forbidden(...)` | 403 — not you |
| `errs.NotFound(...)` | 404 — and not a hint that it exists |
| `errs.PaymentRequired(...)` | 402 |
| `errs.Exhausted(...)` | 429 |

Prefer `NotFound` over `Forbidden` where confirming existence is itself a
leak. "You may not read product 91" tells an attacker that product 91 exists.

The guard's name is attached to the error as log metadata, so a refusal in the
log says which guard refused.

## Next

- [Interceptors](interceptors.md) — around the handler rather than before it.
- [Security](security.md) — guards in the context of the whole surface.
