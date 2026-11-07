# Controllers

A controller reads the request, calls a service, and renders the result. It
holds no business rules: if a condition would also belong in a scheduled job,
it belongs in the service.

## Routes are values

```go
type ProductController struct {
	svc *service.ProductService
}

func New(svc *service.ProductService) *ProductController {
	return &ProductController{svc: svc}
}

func (c *ProductController) Routes() []router.Route {
	return []router.Route{
		router.Get("/", c.list).
			Summary("List products").Tag("catalog").
			QueryParam("page", "Page number").
			QueryParam("limit", "Items per page").
			Returns([]dto.ProductResponse{}),

		router.Post("/", c.create).
			Guard(guard.Tenanted).
			Summary("Create a product").Tag("catalog").
			Accepts(dto.CreateProduct{}).
			Returns(dto.ProductResponse{}, fiber.StatusCreated),

		router.Get("/:id", c.get).
			Summary("Get a product").
			Param("id", "Product id"),

		router.Delete("/:id", c.remove).
			Guard(guard.Tenanted, guard.HasRole("catalog:write")),
	}
}
```

The usual way of registering routes — calling `r.Get(...)` inside a `Register`
function — throws away everything about them the moment it runs. Nothing can
tell you afterwards which routes exist, which are unguarded, or what they do,
because the registration was a side effect rather than a value.

Returning them instead means the same value mounts the route, prints the route
table, feeds the OpenAPI metadata, and can be asserted over in a test.

## The registry

Every mounted route is recorded:

```go
app.Routes.All()        // []router.Mounted, sorted by path then method
app.Routes.Table()      // aligned text for a CLI
app.Routes.Unguarded()  // mutating routes with no guard and no Public marker
app.RoutesJSON()        // for `icpt routes`
app.WriteRoutes("routes.json")
```

The assertion worth writing once per service:

```go
func TestEveryWriteIsGuarded(t *testing.T) {
	if un := app.Routes.Unguarded(); len(un) > 0 {
		t.Fatalf("mutating routes with no guard: %v", un)
	}
}
```

A route that is deliberately public says so:

```go
router.Get("/health/public", c.status).AsPublic()
```

`.AsPublic()` exists so that "no guard because it is public" and "no guard
because somebody forgot" stop looking identical.

## Builders

| | |
|---|---|
| `.Guard(g...)` | Appends guards, in execution order. |
| `.Intercept(i...)` | Appends interceptors; the first added is outermost. |
| `.Use(h...)` | Raw Fiber middleware, for things that are neither. |
| `.Summary(s)` / `.Describe(s)` | Documentation. |
| `.Tag(t...)` | OpenAPI tags. |
| `.ID(s)` | OpenAPI `operationId`. |
| `.Param(name, desc)` | A required path parameter. |
| `.QueryParam(name, desc)` / `.HeaderParam(...)` | Optional parameters. |
| `.Accepts(dto.X{})` | The request body type. |
| `.Returns(dto.Y{}, statuses...)` | The success payload and extra statuses. |
| `.Deprecate()` | Marks the route deprecated. |
| `.AsPublic()` | Marks it intentionally unauthenticated. |

Every builder returns a copy, so a route value can be shared and specialised
without the two interfering.

## Handlers

A handler is a `fiber.Handler`. The shape is always the same: extract, call,
render.

```go
func (c *ProductController) get(ctx fiber.Ctx) error {
	id, err := pipe.UUIDParam(ctx, "id")
	if err != nil {
		return response.Fail(ctx, err)
	}
	p, err := c.svc.Get(ctx.Context(), id)
	if err != nil {
		return response.Fail(ctx, err)
	}
	return response.OK(ctx, constants.MsgFetched, service.ToResponse(p))
}
```

`response.Fail` works out the status, the code and the field details from the
error, so a handler never maps an error to a number. See
[Exception filters](exception-filters.md).

Note `ctx.Context()` — the service takes a `context.Context`, not a
`fiber.Ctx`. That is what lets the same service be called from a scheduled
job.

## Responses

| | |
|---|---|
| `response.OK(c, msg, data)` | 200 |
| `response.Created(c, msg, data)` | 201 |
| `response.Accepted(c, msg, data)` | 202 — queued, not done |
| `response.NoContent(c)` | 204, the one reply that is not an envelope |
| `response.List(c, msg, items, page, size, total)` | 200 with page metadata |
| `response.Fail(c, err)` | Whatever the error says it is |

Use `Accepted` for work that was queued rather than completed. A long-running
import is `202`, not `201`: the record exists but the thing itself does not
yet.

## Multiple controllers in one module

A module can have several, and they share the module's path:

```go
module.WithControllers(
	controller.NewProductController(svc),
	controller.NewProductImageController(imageSvc),   // /:id/images
)
```

Split when a controller grows past comfortable reading, not by HTTP verb.

## Controllers without a struct

For something tiny:

```go
module.WithControllers(router.ControllerFunc(func() []router.Route {
	return []router.Route{
		router.Get("/version", versionHandler).AsPublic(),
	}
}))
```

## What a controller must not do

- **No SQL.** `icpt lint` fails the build. A query in a controller is a query
  no other caller can reuse and no test can exercise without HTTP — and it is
  where the ownership filter gets forgotten.
- **No business rules.** If a scheduled job would need the same check, it
  belongs in the service.
- **No status codes in the service's place.** The service returns
  `errs.Conflict(...)`; the controller just renders it.

## Next

- [Guards](guards.md) · [Interceptors](interceptors.md) · [Pipes](pipes.md)
- [Validation](validation.md) — `binding.Body[T]` and friends.
- [Exception filters](exception-filters.md) — what `response.Fail` does.
