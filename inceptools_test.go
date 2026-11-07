package inceptools_test

import (
	"net/http"
	"sync"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/IncepTools/go/core/ctxkit"
	"github.com/IncepTools/go/core/errs"
	"github.com/IncepTools/go/core/module"
	"github.com/IncepTools/go/testkit"
	"github.com/IncepTools/go/web/binding"
	"github.com/IncepTools/go/web/guard"
	"github.com/IncepTools/go/web/response"
	"github.com/IncepTools/go/web/router"
)

// A whole module, in miniature, so the test exercises the real pipeline:
// mounting, guards, binding, the envelope and the error mapping.

type createNote struct {
	Title string `json:"title" validate:"required,min=3,max=20"`
	Body  string `json:"body" validate:"omitempty,max=100"`
}

type note struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type notesController struct {
	mu    sync.Mutex
	notes []note
}

func (c *notesController) Routes() []router.Route {
	return []router.Route{
		router.Get("/", c.list).Summary("List notes").AsPublic(),
		router.Post("/", c.create).Guard(guard.Tenanted).Summary("Create a note"),
		router.Get("/:id", c.get).Param("id", "Note id").AsPublic(),
		router.Delete("/:id", c.boom).Guard(guard.Tenanted),
	}
}

func (c *notesController) list(ctx fiber.Ctx) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return response.List(ctx, "notes retrieved", c.notes, 1, 20, int64(len(c.notes)))
}

func (c *notesController) create(ctx fiber.Ctx) error {
	req, err := binding.Body[createNote](ctx)
	if err != nil {
		return response.Fail(ctx, err)
	}
	n := note{ID: uuid.NewString(), Title: req.Title}
	c.mu.Lock()
	c.notes = append(c.notes, n)
	c.mu.Unlock()
	return response.Created(ctx, "note created", n)
}

func (c *notesController) get(ctx fiber.Ctx) error {
	return response.Fail(ctx, errs.NotFound("note not found").WithCode("NOTE_NOT_FOUND"))
}

// boom returns an unclassified error, standing in for a driver error that
// escaped a service.
func (c *notesController) boom(ctx fiber.Ctx) error {
	return response.Fail(ctx, errSecret)
}

var errSecret = &plainError{"pq: password authentication failed for user \"svc\" at 10.0.0.9"}

type plainError struct{ s string }

func (e *plainError) Error() string { return e.s }

type notesModule struct{ module.Base }

func newNotesModule() *notesModule {
	return &notesModule{Base: module.NewBase("notes", module.WithControllers(&notesController{}))}
}

type demoApp struct{ module.BaseApp }

func newDemoApp() *demoApp {
	return &demoApp{BaseApp: module.NewApp("demo", newNotesModule())}
}

// tenanted installs a tenant on the context, the way real authentication
// middleware would.
func tenanted() fiber.Handler {
	return func(c fiber.Ctx) error {
		c.SetContext(ctxkit.WithTenant(c.Context(), ctxkit.Tenant{AccountID: uuid.New()}))
		return c.Next()
	}
}

func TestEnvelopeShape(t *testing.T) {
	h := testkit.Mount(t, "/api/v1", newDemoApp())

	res := h.Get("/api/v1/demo/notes").ExpectStatus(t, http.StatusOK)
	if !res.Meta.Success {
		t.Fatalf("meta.success = false on a 200: %s", res.Body)
	}
	if res.Meta.TraceID == "" {
		t.Fatal("no trace id in meta; a client has nothing to quote in a ticket")
	}
	if res.Meta.Page == nil || *res.Meta.Page != 1 {
		t.Fatalf("list response has no page metadata: %s", res.Body)
	}
}

func TestValidationFailureIsFieldScoped(t *testing.T) {
	app := newDemoApp()
	f := fiber.New()
	api := f.Group("/api/v1")
	api.Use(tenanted())
	if err := module.MountApp(api, app, module.MountOptions{Prefix: "/api/v1"}); err != nil {
		t.Fatal(err)
	}
	h := testkit.New(t, f)

	res := h.Post("/api/v1/demo/notes", map[string]any{"title": "ab"}).
		ExpectStatus(t, http.StatusBadRequest).
		ExpectCode(t, "VALIDATION_FAILED").
		ExpectFieldError(t, "title")

	if len(res.Meta.Errors) == 0 || res.Meta.Errors[0].Message == "" {
		t.Fatalf("no usable message on the field error: %s", res.Body)
	}
	if res.Meta.Errors[0].Rule != "min" {
		t.Fatalf("rule = %q, want min", res.Meta.Errors[0].Rule)
	}
}

func TestGuardRefusesWithoutTenant(t *testing.T) {
	h := testkit.Mount(t, "/api/v1", newDemoApp())
	h.Post("/api/v1/demo/notes", map[string]any{"title": "hello"}).
		ExpectStatus(t, http.StatusUnauthorized).
		ExpectCode(t, "NO_TENANT")
}

func TestCreateSucceedsWithTenant(t *testing.T) {
	f := fiber.New()
	api := f.Group("/api/v1")
	api.Use(tenanted())
	if err := module.MountApp(api, newDemoApp(), module.MountOptions{Prefix: "/api/v1"}); err != nil {
		t.Fatal(err)
	}
	h := testkit.New(t, f)

	res := h.Post("/api/v1/demo/notes", map[string]any{"title": "hello world"}).
		ExpectStatus(t, http.StatusCreated)
	var n note
	res.JSON(t, &n)
	if n.Title != "hello world" {
		t.Fatalf("note = %+v", n)
	}
}

func TestTypedNotFoundCarriesItsCode(t *testing.T) {
	h := testkit.Mount(t, "/api/v1", newDemoApp())
	h.Get("/api/v1/demo/notes/"+uuid.NewString()).
		ExpectStatus(t, http.StatusNotFound).
		ExpectCode(t, "NOTE_NOT_FOUND")
}

func TestUnclassifiedErrorDoesNotLeak(t *testing.T) {
	f := fiber.New()
	api := f.Group("/api/v1")
	api.Use(tenanted())
	if err := module.MountApp(api, newDemoApp(), module.MountOptions{Prefix: "/api/v1"}); err != nil {
		t.Fatal(err)
	}
	h := testkit.New(t, f)

	res := h.Delete("/api/v1/demo/notes/"+uuid.NewString()).
		ExpectStatus(t, http.StatusInternalServerError)

	if containsAny(string(res.Body), "password", "10.0.0.9", "pq:") {
		t.Fatalf("the driver error reached the client: %s", res.Body)
	}
	if res.Meta.TraceID == "" {
		t.Fatal("a 500 with no trace id is unactionable")
	}
}

func TestRouteRegistryRecordsPathsAndGuards(t *testing.T) {
	reg := &router.Registry{}
	f := fiber.New()
	if err := module.MountApp(f.Group("/api/v1"), newDemoApp(), module.MountOptions{
		Prefix: "/api/v1", Routes: reg,
	}); err != nil {
		t.Fatal(err)
	}

	byPath := map[string]bool{}
	for _, r := range reg.All() {
		byPath[r.Method+" "+r.FullPath] = true
		if r.Module != "demo.notes" {
			t.Errorf("module = %q, want demo.notes", r.Module)
		}
	}
	for _, want := range []string{"GET /api/v1/demo/notes", "POST /api/v1/demo/notes", "GET /api/v1/demo/notes/:id"} {
		if !byPath[want] {
			t.Errorf("route %q was not recorded; got %v", want, byPath)
		}
	}

	if un := reg.Unguarded(); len(un) != 0 {
		t.Fatalf("unguarded mutating routes: %v", un)
	}
}

func TestUnguardedMutatingRouteIsReported(t *testing.T) {
	type leaky struct{ module.Base }
	mod := &leaky{Base: module.NewBase("leaky", module.WithControllers(
		router.ControllerFunc(func() []router.Route {
			return []router.Route{
				router.Post("/", func(c fiber.Ctx) error { return c.SendStatus(200) }),
			}
		}),
	))}
	app := module.NewApp("demo", mod)

	reg := &router.Registry{}
	f := fiber.New()
	if err := module.MountApp(f, app, module.MountOptions{Routes: reg}); err != nil {
		t.Fatal(err)
	}

	un := reg.Unguarded()
	if len(un) != 1 {
		t.Fatalf("unguarded = %d, want 1 — this check is the cheapest security control in the framework", len(un))
	}
}

func TestDuplicateModuleNameIsRefusedAtMount(t *testing.T) {
	app := module.NewApp("demo", newNotesModule(), newNotesModule())
	f := fiber.New()
	err := module.MountApp(f, app, module.MountOptions{})
	if err == nil {
		t.Fatal("two modules on one path were accepted; the second's routes would never match")
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(sub) > 0 && len(s) >= len(sub) {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}
