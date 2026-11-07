# Architecture

## Three levels

```
App       a bounded domain       catalog, billing, identity
Module    one resource in it     products, invoices, sessions
Layer     controller / service / repository / dto / entity
```

There is no fourth. A module that wants sub-modules is two modules; an app
that wants sub-apps is two apps. The flat rule is worth more than the
expressiveness, because a reader standing in a directory can always tell which
of the three they are in.

## A module

```
apps/catalog/products/
├── products_module.go    name, controllers, entities, guards
├── constants/            module name, messages, sortable columns
├── dto/                  request and response shapes
├── entity/               stored shapes
├── repository/           data access
├── service/              business rules
└── controller/           request handling
```

The module file is the whole declaration:

```go
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
```

`Providers` is a **package function, not a method**. It has to run before any
module is constructed — `New` resolves the service out of the container — and
a method could only run once the module existed, which is too late.

## Two phases, and why

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

Doing it domain by domain would construct the first before the second had
registered anything, so a controller in the first that resolves a service from
the second would fail at boot — for no reason a reader could see, and fixed by
reordering a list, which is the kind of fix that comes back.

## What may import what

| Layer | May import | Must not |
|---|---|---|
| `controller` | service, dto, constants, `web/*` | `gorm.io/*`, `database/sql` |
| `service` | repository, entity, dto, `core/*` | `gofiber/*`, `web/response`, `web/binding` |
| `repository` | entity, `data/*` | `web/*` |
| `dto` | nothing of consequence | `gorm.io/*` |
| `entity` | nothing of consequence | `web/*`, drivers |

`icpt lint` enforces this, and each finding says what breaks:

- **A service that imports HTTP** cannot be called from a scheduled job, a CLI
  or a queue consumer without faking a request. Return an `*errs.Error` and
  let the controller pick the status.
- **A controller that imports GORM** writes a query no other caller can reuse
  and no test can exercise without HTTP. It is also where tenant scoping gets
  forgotten.
- **A DTO that imports the database** is an entity with a different name, and
  returning it leaks the schema into your public API — so adding a column
  becomes an unannounced API change.

## Cross-module coupling

One module importing another's `service` or `entity` is a warning, not an
error, because it is occasionally right. It is usually the first sign that two
modules should be one, or that the shared piece belongs a level up. Depend on
an interface the other module publishes, rather than on its internals.

## Where behaviour belongs

| | |
|---|---|
| Can a scheduled job need it? | **service** |
| Only makes sense for an HTTP request? | **controller** |
| Is it "may this caller do this?" | **guard** |
| Is it "around every request in this module"? | **interceptor** |
| Is it a query? | **repository** |
| Does it need to survive a crash? | **outbox**, in the same transaction |
| Should it not slow the request down? | **event**, async |

## The request pipeline

```
request
  → logger on the context
  → CORS
  → request id         ─ so everything below logs the same id
  → recover            ─ so a panic below is caught with that id
  → security headers
  → access log
  → identity           ─ account and actor on the context; decides nothing
  → timeout
  → rate limit
  → [route] guards     ─ may this caller do this?
  → [route] interceptors
  → handler
  ← response.Send      ─ one envelope, one sanitiser
  ← exception filter   ─ anything that escaped, logged once and classified
```

The order is not a style choice. Request id before recover, or a panic is
logged with no id. Identity before timeout, or an authentication failure is
reported as a timeout. Guards attached per route rather than with `Use`, or
adding a public route below a protected one silently exposes it.

## Next

- [Modules](modules.md) — the declaration in full.
- [Controllers](controllers.md) · [Guards](guards.md) · [Middleware](middleware.md)
- [Database](database.md) — what the repository layer gives you.
