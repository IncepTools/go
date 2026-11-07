package module_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/IncepTools/go/src/core/errs"
	"github.com/IncepTools/go/src/core/lifecycle"
	"github.com/IncepTools/go/src/core/module"
	"github.com/IncepTools/go/src/web/guard"
	"github.com/IncepTools/go/src/web/health"
	"github.com/IncepTools/go/src/web/router"
)

// testModule is a module in miniature: one route, one health check, and a
// shutdown hook, so a mount can be checked for all three at once.
type testModule struct {
	module.Base
	shutdowns *int
}

func newTestModule(name string, shutdowns *int) *testModule {
	m := &testModule{shutdowns: shutdowns}
	m.Base = module.NewBase(name,
		module.WithControllers(router.ControllerFunc(func() []router.Route {
			return []router.Route{
				router.Get("/ping", func(c fiber.Ctx) error {
					return c.SendString(name)
				}).AsPublic(),
			}
		})),
		module.WithHealthChecks(health.Of(name, health.Optional,
			func(context.Context) error { return nil })),
	)
	return m
}

func (m *testModule) OnShutdown(context.Context) error {
	if m.shutdowns != nil {
		*m.shutdowns++
	}
	return nil
}

func TestMountModulesOmitsTheDomainFromThePath(t *testing.T) {
	app := fiber.New()
	api := app.Group("/api/v1")

	err := module.MountModules(api, "calling", "/api/v1",
		[]module.Module{newTestModule("core", nil), newTestModule("routing", nil)},
		module.MountOptions{})
	if err != nil {
		t.Fatal(err)
	}

	// The domain name is deliberately absent from the URL: a host whose API
	// is already rooted at /api/v1 does not want /api/v1/calling/core.
	for _, tc := range []struct{ path, want string }{
		{"/api/v1/core/ping", "core"},
		{"/api/v1/routing/ping", "routing"},
	} {
		resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, tc.path, nil))
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("%s returned %d", tc.path, resp.StatusCode)
		}
		resp.Body.Close()
	}

	// And the domain-prefixed path must NOT exist, or both would work and the
	// API would have two URLs for one route.
	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/api/v1/calling/core/ping", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("the domain-prefixed path returned %d, want 404", resp.StatusCode)
	}
}

func TestMountModulesKeepsTheQualifiedOwnerInTheRouteTable(t *testing.T) {
	app := fiber.New()
	routes := &router.Registry{}

	err := module.MountModules(app.Group("/api/v1"), "calling", "/api/v1",
		[]module.Module{newTestModule("core", nil)},
		module.MountOptions{Prefix: "/api/v1", Routes: routes})
	if err != nil {
		t.Fatal(err)
	}

	all := routes.All()
	if len(all) != 1 {
		t.Fatalf("recorded routes = %d, want 1", len(all))
	}
	// Dropping the domain from the URL must not drop it from the ownership
	// record: "who owns this route" is still calling.core, and that is what
	// `icpt routes` and the unguarded-route test read.
	if all[0].Module != "calling.core" {
		t.Errorf("owner = %q, want calling.core", all[0].Module)
	}
	if all[0].FullPath != "/api/v1/core/ping" {
		t.Errorf("full path = %q, want /api/v1/core/ping", all[0].FullPath)
	}
}

func TestMountModulesCollectsHealthAndLifecycle(t *testing.T) {
	app := fiber.New()
	checks := &health.Registry{}
	hooks := &lifecycle.Registry{}
	shutdowns := 0

	err := module.MountModules(app.Group("/api/v1"), "calling", "/api/v1",
		[]module.Module{newTestModule("core", &shutdowns), newTestModule("routing", &shutdowns)},
		module.MountOptions{Health: checks, Lifecycle: hooks})
	if err != nil {
		t.Fatal(err)
	}

	// A module mounted this way is still a full participant — it keeps its
	// health checks and its lifecycle hooks. Losing those silently is the
	// failure a direct mount could easily introduce.
	report := checks.Run(context.Background())
	if len(report.Checks) != 2 {
		t.Errorf("health checks = %d, want 2", len(report.Checks))
	}
	names := hooks.Names()
	want := map[string]bool{"calling.core": true, "calling.routing": true}
	for _, name := range names {
		delete(want, name)
	}
	if len(want) != 0 {
		t.Errorf("lifecycle hooks %v missing from %v", want, names)
	}

	// Init first: Shutdown deliberately drains only what initialised, so
	// nothing is torn down that never came up.
	if err := hooks.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := hooks.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if shutdowns != 2 {
		t.Errorf("shutdowns = %d, want 2", shutdowns)
	}
}

func TestMountModulesRefusesAmbiguousOrNamelessMounts(t *testing.T) {
	cases := []struct {
		name    string
		appName string
		modules []module.Module
	}{
		{
			name:    "an empty domain name leaves routes with no owner",
			appName: "   ",
			modules: []module.Module{newTestModule("core", nil)},
		},
		{
			name:    "an empty module name has no path to mount at",
			appName: "calling",
			modules: []module.Module{newTestModule("", nil)},
		},
		{
			// The second module's routes would simply never match, and
			// nothing about that is visible at runtime. Catch it at boot.
			name:    "two modules on one path make the second unreachable",
			appName: "calling",
			modules: []module.Module{newTestModule("core", nil), newTestModule("core", nil)},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New()
			err := module.MountModules(app.Group("/api/v1"), tc.appName, "/api/v1",
				tc.modules, module.MountOptions{})
			if err == nil {
				t.Fatal("MountModules accepted a mount it cannot serve correctly")
			}
		})
	}
}

func TestMountModulesSkipsNilModules(t *testing.T) {
	app := fiber.New()
	err := module.MountModules(app.Group("/api/v1"), "calling", "/api/v1",
		[]module.Module{nil, newTestModule("core", nil), nil},
		module.MountOptions{})
	if err != nil {
		t.Fatalf("a nil entry in the slice failed the mount: %v", err)
	}

	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/api/v1/core/ping", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("the real module did not mount: %d", resp.StatusCode)
	}
}

func TestMountModulesAppliesCallerGuardsToEveryRoute(t *testing.T) {
	app := fiber.New()
	routes := &router.Registry{}
	denied := false
	blocking := guard.Of("test-blocking", func(fiber.Ctx) error {
		denied = true
		return errs.Forbidden("not you")
	})

	err := module.MountModules(app.Group("/api/v1"), "calling", "/api/v1",
		[]module.Module{newTestModule("core", nil)},
		module.MountOptions{Prefix: "/api/v1", Routes: routes, Guards: []guard.Guard{blocking}})
	if err != nil {
		t.Fatal(err)
	}

	// A guard passed by the host must reach modules mounted this way, or a
	// direct mount becomes a way to bypass application-wide authorization.
	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/api/v1/core/ping", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if !denied {
		t.Fatal("the caller's guard did not run")
	}
}
