# Testing

## Module tests

`testkit` assembles the application in memory and drives it through Fiber's
own test transport:

```go
func TestCreateRequiresATenant(t *testing.T) {
    h := testkit.Mount(t, "/api/v1", catalog.NewApp(scope))

    h.Post("/api/v1/catalog/products", map[string]any{"title": "ab"}).
        ExpectStatus(t, http.StatusUnauthorized).
        ExpectCode(t, "NO_TENANT")
}
```

These are not unit tests with the HTTP layer mocked away. They go through the
real middleware, the real guards and the real exception filter — which is
where most of this framework's behaviour lives, and where a mock quietly
diverges from what ships.

For a test of the whole service, build the real `inceptools.Application` and
hand its Fiber app to `testkit.New`. `testkit.Mount` mounts the modules with
no middleware beyond what they declare, which is right for a module test and
wrong for an integration one.

## Assertions

```go
res := h.Post("/api/v1/catalog/products", body).
    ExpectStatus(t, http.StatusBadRequest).
    ExpectCode(t, "VALIDATION_FAILED").
    ExpectFieldError(t, "title")

var product dto.ProductResponse
res.JSON(t, &product)
```

Every failure prints the body, which is what you actually want to see when a
status is not what you expected.

## Service and repository tests

```go
ctx := testkit.Context(accountID, userID, "catalog:write")
got, err := svc.Create(ctx, dto.CreateProduct{Name: "Desk lamp"})
```

`testkit.Context` carries a tenant, an actor and a trace id — everything the
scoped repository and the logger read.

## Structural tests

These are worth writing once per service.

**Every write is guarded.** The cheapest security control available: the
failure it catches — a write endpoint shipped without authentication — is both
easy to commit and expensive to discover.

```go
func TestEveryWriteIsGuarded(t *testing.T) {
    if un := app.Routes.Unguarded(); len(un) > 0 {
        t.Fatalf("mutating routes with no guard: %v", un)
    }
}
```

A route that is deliberately public says so with `.AsPublic()`, so "no guard
because it is public" and "no guard because somebody forgot" stop looking
identical.

**The layering holds.** `icpt lint -strict` in CI, or shell out to it from a
test.

**No two modules claim one table.**

```go
owners := module.OwnedEntities(apps...)
```

## Naming

`TestCreate` tells a reader nothing. `TestReusingAKeyWithADifferentBodyIsRefused`
tells them what is guaranteed. Write the failure into the name, and write the
reason into the failure message:

```go
t.Fatalf("handler ran %d times; a retried POST must not provision twice", n)
```

## The race detector

`make test` runs with `-race`, and CI does too. The DI container, the event
bus, the scheduler and the circuit breaker are all concurrent; a data race in
any of them is a production defect that no single-threaded test would show.

## Next

- [Controllers](controllers.md) — the route values the structural tests read.
- [Providers](providers.md) — substituting a dependency in a test.
