// Package inceptools is the application factory: the one call that turns a
// set of modules into a running, observable, gracefully-stopping HTTP service.
//
// One call replaces the hundred lines of wiring every service otherwise
// writes for itself. What it assembles, in the order that is correct:
//
//	container        → providers registered, nothing constructed yet
//	middleware       → request id, recover, headers, logging, identity, timeout
//	routes           → every app mounted under the version prefix, and recorded
//	health           → /healthz (liveness), /readyz (readiness), /health (detail)
//	lifecycle        → OnInit, listen, OnReady, signal, drain, OnShutdown
//
// A minimal service:
//
//	func main() {
//	    var cfg Config
//	    if err := config.Load(&cfg); err != nil { log.Fatal(err) }
//
//	    app := inceptools.New(inceptools.Options{
//	        Service: "compute", Version: build.Version,
//	        Addr: fmt.Sprintf(":%d", cfg.Port), Prefix: "/api/v1",
//	    })
//	    di.MustSupply(app.Container, db)
//	    if err := app.Mount(catalog.Descriptor(), billing.Descriptor()); err != nil { log.Fatal(err) }
//	    if err := app.Run(context.Background()); err != nil { log.Fatal(err) }
//	}
package inceptools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/IncepTools/go/src/core/di"
	"github.com/IncepTools/go/src/core/lifecycle"
	"github.com/IncepTools/go/src/core/logx"
	"github.com/IncepTools/go/src/core/module"
	"github.com/IncepTools/go/src/web/filter"
	"github.com/IncepTools/go/src/web/guard"
	"github.com/IncepTools/go/src/web/health"
	"github.com/IncepTools/go/src/web/interceptor"
	"github.com/IncepTools/go/src/web/middleware"
	"github.com/IncepTools/go/src/web/response"
	"github.com/IncepTools/go/src/web/router"
)

// Version is the framework's own version, reported on the service's health
// detail endpoint so you can tell which build of the framework is deployed.
const Version = "1.0.0"

// Options configures the application.
//
// Every field has a defensible zero value except Service: a service with no
// name produces logs nobody can route and metrics nobody can group.
type Options struct {
	// Service is this service's name. Required.
	Service string
	// Version is the build version, reported on the health endpoints.
	Version string
	// Addr is the listen address, ":8080" by default.
	Addr string
	// Prefix is mounted in front of every app, e.g. "/api/v1". Empty mounts
	// at the root, which is a choice you will regret the first time you need
	// a v2 — set it.
	Prefix string

	// Logger defaults to a JSON logger at info level on stdout.
	Logger *slog.Logger

	// Fiber overrides the server config. ErrorHandler is set for you unless
	// you provide one; overriding it opts out of the envelope and the leak
	// check, so do it knowingly.
	Fiber fiber.Config

	// Middleware configures the standard chain.
	Middleware middleware.ChainOptions
	// CORS, when set, is installed before everything else.
	CORS *middleware.CORSOptions
	// RateLimit, when set, is installed after the identity middleware so its
	// key function can see who is calling.
	RateLimit *middleware.RateLimitOptions
	// Filters are exception filters consulted before the built-in mapping.
	Filters []filter.Filter

	// Guards apply to every route of every mounted app.
	Guards []guard.Guard
	// Interceptors wrap every route of every mounted app.
	Interceptors []interceptor.Interceptor

	// Sanitizer scrubs outgoing messages. Install one in any service whose
	// errors can carry infrastructure detail — which is all of them.
	Sanitizer response.Sanitizer

	// DrainTimeout bounds shutdown. Default 25s.
	DrainTimeout time.Duration
	// DisableHealthRoutes skips /healthz, /readyz and /health, for a service
	// that publishes its own.
	DisableHealthRoutes bool
	// HealthPrefix moves the health endpoints. Default "" (service root), so
	// they sit OUTSIDE the version prefix — a probe should not have to be
	// updated when the API is versioned.
	HealthPrefix string
}

// Application is a configured, not-yet-listening service.
type Application struct {
	// Fiber is the underlying server. Reach for it when the framework does
	// not cover something; everything here is additive, nothing is hidden.
	Fiber *fiber.App
	// Container holds the providers.
	Container *di.Container
	// Routes is the mounted route table.
	Routes *router.Registry
	// Health aggregates readiness checks.
	Health *health.Registry
	// Lifecycle holds init/ready/shutdown hooks.
	Lifecycle *lifecycle.Registry
	// Log is the application logger.
	Log *slog.Logger

	opt    Options
	api    fiber.Router
	apps   []module.App
	closed bool
}

// New builds the application. It does not listen; call Run.
func New(opt Options) *Application {
	if opt.Service == "" {
		opt.Service = "service"
	}
	if opt.Addr == "" {
		opt.Addr = ":8080"
	}
	if opt.DrainTimeout <= 0 {
		opt.DrainTimeout = lifecycle.DefaultDrainTimeout
	}
	log := opt.Logger
	if log == nil {
		log = logx.New(logx.Options{Level: slog.LevelInfo, Format: logx.FormatJSON, Service: opt.Service})
	}
	if opt.Sanitizer != nil {
		response.SetSanitizer(opt.Sanitizer)
	}

	fcfg := opt.Fiber
	if fcfg.ErrorHandler == nil {
		fcfg.ErrorHandler = filter.ErrorHandler(filter.Options{Filters: opt.Filters})
	}
	if fcfg.AppName == "" {
		fcfg.AppName = opt.Service
	}
	// Fiber's default is to trust no proxy headers. Leave that alone: a
	// framework that trusts X-Forwarded-For by default hands every rate
	// limiter and audit log a client-controlled IP.

	f := fiber.New(fcfg)

	app := &Application{
		Fiber:     f,
		Container: di.New(),
		Routes:    &router.Registry{},
		Health:    health.New(opt.Service, opt.Version),
		Lifecycle: &lifecycle.Registry{},
		Log:       log,
		opt:       opt,
	}

	// Put the logger on every request context so logx.C works at any depth.
	f.Use(func(c fiber.Ctx) error {
		c.SetContext(logx.Into(c.Context(), log))
		return c.Next()
	})
	if opt.CORS != nil {
		f.Use(middleware.CORS(*opt.CORS))
	}
	for _, h := range middleware.Chain(opt.Middleware) {
		f.Use(h)
	}
	if opt.RateLimit != nil {
		f.Use(middleware.RateLimit(*opt.RateLimit))
	}

	if !opt.DisableHealthRoutes {
		hr := fiber.Router(f)
		if opt.HealthPrefix != "" {
			hr = f.Group(opt.HealthPrefix)
		}
		hr.Get("/healthz", health.LiveHandler())
		hr.Get("/readyz", health.ReadyHandler(app.Health))
		hr.Get("/health", health.DetailHandler(app.Health))
	}

	if opt.Prefix != "" {
		app.api = f.Group(opt.Prefix)
	} else {
		app.api = f
	}

	di.MustSupply(app.Container, log)
	return app
}

// API returns the router every app is mounted on — the version prefix group.
// Use it for the rare route that belongs to no module.
func (a *Application) API() fiber.Router { return a.api }

// Mount registers every domain's providers, then builds the domains, then
// mounts their routes — in that order, for every domain, not domain by
// domain.
//
// Doing it domain by domain would construct the first before the second had
// registered anything, so a controller in the first that resolves a service
// from the second would fail at boot for no reason a reader could see.
func (a *Application) Mount(descriptors ...module.Descriptor) error {
	if err := module.Register(a.Container, descriptors...); err != nil {
		return err
	}
	apps, err := module.Build(di.NewScope(a.Container), descriptors...)
	if err != nil {
		return err
	}
	return a.MountApps(apps...)
}

// MountApps mounts already-constructed domains.
//
// Use it when the domains need no container — a test, or a service small
// enough to wire by hand. Everything else should use Mount, which gets the
// two phases right for you.
func (a *Application) MountApps(apps ...module.App) error {
	for _, app := range apps {
		err := module.MountApp(a.api, app, module.MountOptions{
			Prefix:       a.opt.Prefix,
			Routes:       a.Routes,
			Health:       a.Health,
			Lifecycle:    a.Lifecycle,
			Guards:       a.opt.Guards,
			Interceptors: a.opt.Interceptors,
		})
		if err != nil {
			return err
		}
	}
	a.apps = append(a.apps, apps...)
	return nil
}

// Seed runs every module's Seeder. Call it after migrations, before Run.
func (a *Application) Seed(ctx context.Context) error { return module.Seed(ctx, a.apps...) }

// Run starts the listener and blocks until a signal arrives or the listener
// fails, then drains.
//
// The sequence is deliberate and is the reason to use this rather than
// f.Listen directly: eager construction before the socket opens, so a bad
// dependency fails the deploy; the 404 handler added after every module, so a
// late-mounted route is not shadowed; lifecycle hooks around the listener; and
// a drain context that is NOT derived from the cancelled one.
func (a *Application) Run(ctx context.Context) error {
	// Construct everything now. A provider that fails here fails the boot,
	// loudly, instead of failing the first request that happens to need it.
	if err := a.Container.Eager(); err != nil {
		return fmt.Errorf("inceptools: wiring failed: %w", err)
	}

	// Registered last so it cannot shadow a module route.
	a.Fiber.Use(filter.NotFoundHandler())

	a.Log.Info("starting",
		"service", a.opt.Service,
		"version", a.opt.Version,
		"framework", Version,
		"addr", a.opt.Addr,
		"prefix", a.opt.Prefix,
		"routes", len(a.Routes.All()),
	)
	if un := a.Routes.Unguarded(); len(un) > 0 {
		// Not fatal — a deployment should not be blocked by a lint rule at
		// 3am — but loud, and a test should make it fatal in CI.
		for _, r := range un {
			a.Log.Warn("mutating route has no guard",
				"method", r.Method, "path", r.FullPath, "module", r.Module)
		}
	}

	serve := func() error {
		err := a.Fiber.Listen(a.opt.Addr, fiber.ListenConfig{DisableStartupMessage: true})
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		return nil
	}
	stop := func(c context.Context) error {
		return a.Fiber.ShutdownWithContext(c)
	}

	runErr := lifecycle.Run(ctx, a.Lifecycle, serve, stop, a.opt.DrainTimeout)

	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), a.opt.DrainTimeout)
	defer cancel()
	closeErr := a.Container.Close(closeCtx)

	a.Log.Info("stopped")
	return errors.Join(runErr, closeErr)
}

// Shutdown stops the listener and closes the container. Run does this for
// you; this is for an embedded or test-driven application.
func (a *Application) Shutdown(ctx context.Context) error {
	if a.closed {
		return nil
	}
	a.closed = true
	return errors.Join(
		a.Fiber.ShutdownWithContext(ctx),
		a.Lifecycle.Shutdown(ctx),
		a.Container.Close(ctx),
	)
}

// RouteTable renders the mounted routes, for a CLI or a boot-time dump.
func (a *Application) RouteTable() string { return a.Routes.Table() }

// RoutesJSON renders the mounted route table as JSON, for `icpt routes`.
func (a *Application) RoutesJSON() ([]byte, error) {
	type routeJSON struct {
		Method     string   `json:"method"`
		Path       string   `json:"path"`
		FullPath   string   `json:"full_path"`
		Module     string   `json:"module"`
		GuardNames []string `json:"guards"`
		Public     bool     `json:"public"`
	}
	routes := a.Routes.All()
	serialized := make([]routeJSON, 0, len(routes))
	for _, route := range routes {
		serialized = append(serialized, routeJSON{
			Method: route.Method, Path: route.Path, FullPath: route.FullPath,
			Module: route.Module, GuardNames: route.GuardNames, Public: route.Public,
		})
	}
	return json.MarshalIndent(struct {
		Service string      `json:"service"`
		Version string      `json:"version"`
		Routes  []routeJSON `json:"routes"`
	}{Service: a.opt.Service, Version: a.opt.Version, Routes: serialized}, "", "  ")
}

// WriteRoutes writes the route table to a file.
//
// Call it from a build step or behind a flag and commit the result: the dump
// is a diffable record of your public surface, so an accidentally added,
// removed or unguarded route shows up in code review rather than in
// production.
func (a *Application) WriteRoutes(path string) error {
	raw, err := a.RoutesJSON()
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}
