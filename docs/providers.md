# Providers

`core/di` is the dependency-injection container: typed, lazy providers
resolved through generics. No reflection, no struct tags, no autowiring.

```go
c := di.New()

di.MustSupply(c, db)                     // something already built

di.MustProvide(c, func(s *di.Scope) (*ProductRepo, error) {
	return NewProductRepo(di.MustResolve[*gorm.DB](s)), nil
})

repo := di.MustResolve[*ProductRepo](c)
```

## What it gives you over a bootstrap function

**Lazy, exactly once.** A provider runs the first time something asks for it,
once, even under concurrent resolution.

**Cycles reported, not hung.** Two providers that depend on each other produce
an error naming the full path, rather than a deadlock or a stack overflow:

```
di: dependency cycle: *catalog.Service -> *billing.Service -> *catalog.Service
```

**Ordered teardown.** Anything implementing `Closer` or `io.Closer` is closed
in the reverse of its construction order, so a pool never outlives the thing
draining into it.

**Duplicates refused.** Registering the same type twice is an error, not a
silent overwrite. Two providers for one type means two different objects
depending on resolution order — a bug that only shows up in production.

## Supplying versus providing

`Supply` registers a value you already have. `Provide` registers a constructor
that runs on demand.

```go
di.MustSupply(c, db)        // the *gorm.DB you opened in main
di.MustSupply(c, logger)
di.MustSupply[cache.Cache](c, cache.NewMemory(time.Minute))
```

The explicit type parameter in the last line matters: without it the binding
is registered under `*cache.Memory`, and a constructor asking for
`cache.Cache` will not find it. **Register an implementation under the
interface its consumers ask for.**

## Named bindings

One type, two instances:

```go
di.MustProvideNamed(c, "primary", func(*di.Scope) (*gorm.DB, error) { ... })
di.MustProvideNamed(c, "replica", func(*di.Scope) (*gorm.DB, error) { ... })

reader := di.MustResolveNamed[*gorm.DB](c, "replica")
```

## Must versus not

`MustResolve` panics. That is the right call inside a constructor and inside
module wiring, which run at boot: a missing provider there should stop the
process, loudly. It is the wrong call in a request path.

```go
// Boot. Panic is correct — the deploy should fail.
svc := di.MustResolve[*service.Product](s)

// Request path. Handle it.
svc, err := di.Resolve[*service.Product](c)
```

## Eager construction

`Application.Run` calls `Container.Eager()` before the listener opens, so a
provider that fails — a bad DSN, an unreachable broker — fails the deploy
rather than the first request that happens to need it.

Nothing is lazy by the time you are serving traffic. Laziness is about
registration order, not about deferring failure.

## Teardown

```go
type Pool struct{ ... }

func (p *Pool) Close(ctx context.Context) error {
	return p.drain(ctx)
}
```

A provider whose value implements `di.Closer` (or `io.Closer`) is closed by
`Container.Close`, in reverse construction order, with the drain deadline as
its context. `Application.Run` calls it for you.

## Scopes

A constructor receives a `*di.Scope` rather than the container. Resolving
through the scope extends the resolution path, which is what makes cycle
reporting possible. Outside a constructor — in module wiring, in a test — use
`di.NewScope(container)`.

## Diagnostics

```go
c.Providers()   // every registered binding, for a boot-time dump
di.Has[*ProductRepo](c)
```

## What it deliberately will not do

Reflection-based autowiring by struct tag. A constructor asks for what it
needs, in code, and a missing dependency is a boot-time error naming both
sides. The alternative saves three lines per type and costs you a runtime
failure whose cause is a tag nobody read.

## Interfaces and testing

Depend on an interface where you want to substitute in tests, and register the
real implementation under it:

```go
type Clock interface{ Now() time.Time }

di.MustSupply[Clock](c, realClock{})      // production
di.MustSupply[Clock](c, fixedClock{t})    // test
```

For a service test you usually do not need the container at all — construct
the service directly with fakes. The container is for assembling the
application, not for every test to go through.

## Next

- [Modules](modules.md) — the two phases this container's laziness exists for.
- [Lifecycle](lifecycle.md) — what happens after everything is constructed.
