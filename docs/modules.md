# Modules

A module is one resource: its routes, its rules, its data access, and the
declaration that ties them together.

```
App       a bounded domain       catalog, billing, identity
Module    one resource in it     products, invoices, sessions
Layer     controller / service / repository / dto / entity
```

There is no fourth level. A module that wants sub-modules is two modules; an
app that wants sub-apps is two apps. The flat rule is worth more than the
expressiveness, because a reader standing in a directory can always tell which
of the three they are in.

## The module file

```go
package products

type Module struct{ module.Base }

func Providers(c *di.Container) error {
	if err := di.Provide(c, func(s *di.Scope) (*repository.ProductRepository, error) {
		return repository.NewProductRepository(di.MustResolve[*gorm.DB](s)), nil
	}); err != nil {
		return err
	}
	return di.Provide(c, func(s *di.Scope) (*service.ProductService, error) {
		return service.NewProductService(di.MustResolve[*repository.ProductRepository](s)), nil
	})
}

func New(s *di.Scope) *Module {
	ctrl := controller.NewProductController(di.MustResolve[*service.ProductService](s))
	return &Module{Base: module.NewBase("products",
		module.WithControllers(ctrl),
		module.WithEntities(&entity.Product{}),
		module.WithGuards(guard.Authenticated),
	)}
}

var (
	_ module.Module      = (*Module)(nil)
	_ module.EntityOwner = (*Module)(nil)
)
```

The compile-time assertions at the bottom are worth keeping. A typo in a
method name is then a build failure, not a route that quietly never mounts.

## Why `Providers` is a package function

It has to run before any module is constructed. `New` resolves the service out
of the container, so the service must already be registered — and a method on
the module could only run once the module existed, which is too late.

## Two phases

```go
func Descriptor() module.Descriptor {
	return module.Descriptor{
		Providers: Providers,
		Build:     func(s *di.Scope) module.App { return NewApp(s) },
	}
}
```

`Application.Mount` runs **every** descriptor's `Providers`, then **every**
`Build`, then mounts the routes. Not domain by domain.

Domain by domain would construct the first before the second had registered
anything, so a controller in the first that resolves a service from the second
would fail at boot — for no reason a reader could see, and fixed by reordering
a list. That is the kind of fix that comes back.

## What a module can declare

Only `Name` and `Controllers` are required; everything else is an optional
interface the framework checks for.

| Declaration | Interface | Effect |
|---|---|---|
| `module.WithControllers(...)` | `Module` | Mounts the routes under `/<app>/<module>`. |
| `module.WithEntities(...)` | `EntityOwner` | Records which tables this module owns. |
| `module.WithGuards(...)` | `Guarded` | Applies guards to every route in the module. |
| `module.WithInterceptors(...)` | `Intercepted` | Wraps every route in the module. |
| `module.WithHealthChecks(...)` | `HealthContributor` | Adds to the readiness report. |
| `Seed(ctx) error` | `Seeder` | Inserts reference data after migrations. |
| `OnInit`, `OnReady`, `OnShutdown` | `lifecycle.*` | Participates in start-up and drain. |

### Entities

`WithEntities` is metadata, not migration. The schema comes from versioned
SQL, because `AutoMigrate` against a production database is a way to lose a
column. What the list buys you is an answerable question — "who owns this
table?" — and a check that catches two modules claiming the same one:

```go
owners := module.OwnedEntities(apps...)   // "catalog.products" -> []any{&entity.Product{}}
```

### Seeders

```go
func (m *Module) Seed(ctx context.Context) error {
	return m.svc.EnsureDefaultCategories(ctx)
}
```

Seeders run after migrations, so the tables exist, and they run on **every**
boot — so they must be idempotent. `app.Seed(ctx)` runs them in declaration
order and stops at the first failure.

### Lifecycle

A module that implements any of the lifecycle interfaces is registered
automatically when it is mounted:

```go
func (m *Module) OnReady(ctx context.Context) error {
	return m.reconciler.Start(ctx)   // after the listener is up
}

func (m *Module) OnShutdown(ctx context.Context) error {
	return m.reconciler.Stop(ctx)    // before its dependencies go
}
```

Start background loops in `OnReady`, not `OnInit`: a loop that begins before
the process is serving races the boot it is part of. See
[Lifecycle](lifecycle.md).

## A module with no HTTP surface

Return `nil` controllers and implement `lifecycle.Ready`. A pure worker is a
perfectly good module; it just has no routes.

```go
func New(s *di.Scope) *Module {
	return &Module{
		Base: module.NewBase("reconciler"),
		svc:  di.MustResolve[*service.Reconciler](s),
	}
}

func (m *Module) OnReady(ctx context.Context) error { return m.svc.Start(ctx) }
```

## Paths

A module's routes are mounted at `<prefix>/<app>/<module>`. The path is
derived from the names rather than configured per route, which is the trade
this framework makes everywhere: give up the freedom to put a route anywhere,
get a URL you can predict from the directory you are standing in.

Two modules with the same name in one app is an error at mount time, not a
surprise at runtime. Without the check, the second module's routes simply
never match and nothing says so.

## Naming

- Module names are lower-case path segments: `products`, `invoices`,
  `api-keys`. Not `ProductsModule`, not `Product`.
- Be consistent about singular or plural across the codebase. Either is fine;
  mixing them means guessing at every URL.
- The Go package name matches the module name.

## Next

- [Providers](providers.md) — the container the two phases exist for.
- [Controllers](controllers.md) — what `WithControllers` takes.
- [Architecture](architecture.md) — what each layer may import.
