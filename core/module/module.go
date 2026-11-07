// Package module is the unit of organisation: a feature slice that declares
// what it provides, routes, owns and needs checked.
//
// The shape is three levels and no more:
//
//	App     a bounded domain          catalog, billing, identity
//	Module  one resource in it        products, categories, reviews
//	Layer   controller / service / repository / dto / entity
//
// A module owns exactly one resource. It declares what it provides, what it
// routes, which tables it owns and what it needs checked for health — and
// nothing outside it may reach into its service or its entities. The
// enforcement is social, plus `icpt lint`, plus the fact that a module's
// dependencies are explicit in its constructor rather than implicit in a
// shared package.
//
// Why bother, in a language with packages? Because a package boundary says
// nothing about ordering, lifecycle or ownership. This says: these providers
// are constructed before those routes exist, this module's shutdown runs
// before its dependencies', and these two tables belong to this code and no
// other.
package module

import (
	"context"
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/IncepTools/go/core/di"
	"github.com/IncepTools/go/core/lifecycle"
	"github.com/IncepTools/go/web/guard"
	"github.com/IncepTools/go/web/health"
	"github.com/IncepTools/go/web/interceptor"
	"github.com/IncepTools/go/web/router"
)

// Module is one resource: its routes, and whatever of the optional interfaces
// below it needs.
type Module interface {
	// Name is the path segment and the registry key. Lower-case, singular or
	// plural but consistent across the codebase: "products", not "ProductsModule".
	Name() string
	// Controllers declare the routes. A module with no HTTP surface — a pure
	// background worker — returns nil and implements lifecycle.Ready instead.
	Controllers() []router.Controller
}

// Descriptor is how a domain is handed to the application: as two functions
// rather than as a constructed value.
//
// The split is not ceremony, it is a correctness requirement. Building a
// module constructs its controller, which resolves its service out of the
// container — so every provider in the WHOLE application must be registered
// before ANY module is built. One value that both registers and constructs
// cannot express that ordering: whichever domain is listed first would be
// built before the second had registered anything, and would fail to resolve
// a dependency that exists.
//
//	func App() module.Descriptor {
//	    return module.Descriptor{
//	        Providers: Providers,
//	        Build: func(s *di.Scope) module.App {
//	            return &App{BaseApp: module.NewApp("catalog", products.New(s))}
//	        },
//	    }
//	}
type Descriptor struct {
	// Providers registers everything the domain's modules resolve. It runs
	// for every descriptor before any Build runs.
	Providers func(c *di.Container) error
	// Build constructs the domain. By the time it runs, every provider in
	// the application is registered.
	Build func(s *di.Scope) App
}

// EntityOwner declares the tables this module owns.
//
// It is metadata, not migration: schema comes from versioned SQL, because
// AutoMigrate against a production database is a way to lose a column. What
// the list buys you is an answerable question — "who owns this table?" — and
// a lint rule that catches two modules claiming the same one.
type EntityOwner interface {
	Entities() []any
}

// Seeder inserts reference data. It runs after migrations, so the tables
// exist, and it MUST be idempotent: it runs on every boot.
type Seeder interface {
	Seed(ctx context.Context) error
}

// Guarded declares guards applied to every route in the module.
type Guarded interface {
	Guards() []guard.Guard
}

// Intercepted declares interceptors wrapping every route in the module.
type Intercepted interface {
	Interceptors() []interceptor.Interceptor
}

// HealthContributor declares the dependencies this module needs in order to
// work. They are aggregated into the service-wide readiness report.
type HealthContributor interface {
	HealthChecks() []health.Checker
}

// App is a bounded domain: a group of modules under one path segment.
type App interface {
	Name() string
	Modules() []Module
}

// Base implements the boilerplate of Module. Embed it:
//
//	type Module struct {
//	    module.Base
//	    ctrl *controller.Product
//	}
//
//	func New(s *di.Scope) *Module {
//	    ctrl := controller.New(di.MustResolve[*service.Product](s))
//	    return &Module{Base: module.NewBase("products",
//	        module.WithControllers(ctrl),
//	        module.WithEntities(&entity.Product{}),
//	    )}
//	}
type Base struct {
	name         string
	controllers  []router.Controller
	entities     []any
	guards       []guard.Guard
	interceptors []interceptor.Interceptor
	checks       []health.Checker
}

// Option configures a Base.
type Option func(*Base)

// WithControllers attaches controllers.
func WithControllers(c ...router.Controller) Option {
	return func(b *Base) { b.controllers = append(b.controllers, c...) }
}

// WithEntities declares owned tables.
func WithEntities(e ...any) Option {
	return func(b *Base) { b.entities = append(b.entities, e...) }
}

// WithGuards applies guards to every route in the module.
func WithGuards(g ...guard.Guard) Option {
	return func(b *Base) { b.guards = append(b.guards, g...) }
}

// WithInterceptors wraps every route in the module.
func WithInterceptors(i ...interceptor.Interceptor) Option {
	return func(b *Base) { b.interceptors = append(b.interceptors, i...) }
}

// WithHealthChecks contributes readiness checks.
func WithHealthChecks(c ...health.Checker) Option {
	return func(b *Base) { b.checks = append(b.checks, c...) }
}

// NewBase builds the embedded base of a module.
func NewBase(name string, opts ...Option) Base {
	b := Base{name: name}
	for _, o := range opts {
		o(&b)
	}
	return b
}

// Name implements Module.
func (b Base) Name() string { return b.name }

// Controllers implements Module.
func (b Base) Controllers() []router.Controller { return b.controllers }

// Entities implements EntityOwner.
func (b Base) Entities() []any { return b.entities }

// Guards implements Guarded.
func (b Base) Guards() []guard.Guard { return b.guards }

// Interceptors implements Intercepted.
func (b Base) Interceptors() []interceptor.Interceptor { return b.interceptors }

// HealthChecks implements HealthContributor.
func (b Base) HealthChecks() []health.Checker { return b.checks }

// BaseApp implements the boilerplate of App.
type BaseApp struct {
	name    string
	modules []Module
}

// NewApp builds a domain from its modules. Order matters only for lifecycle
// and shutdown; routing is by path.
func NewApp(name string, modules ...Module) BaseApp {
	return BaseApp{name: name, modules: modules}
}

// Name implements App.
func (a BaseApp) Name() string { return a.name }

// Modules implements App.
func (a BaseApp) Modules() []Module { return a.modules }

// MountOptions configures mounting.
type MountOptions struct {
	// Prefix is the path r already sits at, for the recorded route table.
	Prefix string
	// Routes records every mounted route. Strongly recommended: it is what
	// `icpt routes` and the unguarded-route test read.
	Routes *router.Registry
	// Health collects each module's readiness checks.
	Health *health.Registry
	// Lifecycle collects each module's init/ready/shutdown hooks.
	Lifecycle *lifecycle.Registry
	// Guards apply to every route of every module mounted in this call.
	Guards []guard.Guard
	// Interceptors wrap every route of every module mounted in this call.
	Interceptors []interceptor.Interceptor
}

// MountApp mounts one domain at /<app name> and each of its modules at
// /<app name>/<module name>.
//
// The path is derived from the names rather than configured per route, which
// is the trade this framework makes everywhere: give up the freedom to put a
// route anywhere, get a URL you can predict from the directory you are
// standing in.
func MountApp(r fiber.Router, a App, opt MountOptions) error {
	if a == nil {
		return nil
	}
	appName := strings.Trim(a.Name(), "/")
	if appName == "" {
		return fmt.Errorf("module: app has an empty name")
	}
	appRouter := r.Group("/" + appName)
	appPrefix := joinPath(opt.Prefix, "/"+appName)

	seen := map[string]bool{}
	for _, m := range a.Modules() {
		if m == nil {
			continue
		}
		name := strings.Trim(m.Name(), "/")
		if name == "" {
			return fmt.Errorf("module: app %q contains a module with an empty name", appName)
		}
		// Two modules on one path means the second's routes are unreachable,
		// and nothing about that failure is visible at runtime — the routes
		// simply never match. Catch it at boot.
		if seen[name] {
			return fmt.Errorf("module: app %q registers two modules named %q", appName, name)
		}
		seen[name] = true

		if err := mountModule(appRouter, appName, appPrefix, m, opt); err != nil {
			return err
		}
	}
	if opt.Lifecycle != nil {
		opt.Lifecycle.Observe(appName, a)
	}
	return nil
}

func mountModule(appRouter fiber.Router, appName, appPrefix string, m Module, opt MountOptions) error {
	name := strings.Trim(m.Name(), "/")
	modRouter := appRouter.Group("/" + name)
	modPrefix := joinPath(appPrefix, "/"+name)
	qualified := appName + "." + name

	guards := append([]guard.Guard(nil), opt.Guards...)
	if g, ok := m.(Guarded); ok {
		guards = append(guards, g.Guards()...)
	}
	intercepts := append([]interceptor.Interceptor(nil), opt.Interceptors...)
	if i, ok := m.(Intercepted); ok {
		intercepts = append(intercepts, i.Interceptors()...)
	}

	for _, ctrl := range m.Controllers() {
		if ctrl == nil {
			continue
		}
		err := router.MountController(modRouter, ctrl, router.MountOptions{
			Module:       qualified,
			Prefix:       modPrefix,
			Registry:     opt.Routes,
			Guards:       guards,
			Interceptors: intercepts,
		})
		if err != nil {
			return fmt.Errorf("module %s: %w", qualified, err)
		}
	}

	if opt.Health != nil {
		if hc, ok := m.(HealthContributor); ok {
			opt.Health.Register(hc.HealthChecks()...)
		}
	}
	if opt.Lifecycle != nil {
		opt.Lifecycle.Observe(qualified, m)
	}
	return nil
}

// Register runs every descriptor's Providers, in order, before anything is
// constructed.
//
// Registration is separate from construction and construction is lazy, so two
// domains may depend on each other's services regardless of the order they
// are listed in — and a genuine cycle between them is reported as a cycle
// rather than resolved by luck.
func Register(c *di.Container, descriptors ...Descriptor) error {
	for i, d := range descriptors {
		if d.Providers == nil {
			continue
		}
		if err := d.Providers(c); err != nil {
			return fmt.Errorf("module: registering providers for descriptor %d: %w", i, err)
		}
	}
	return nil
}

// Build constructs every domain. Call it only after Register.
func Build(s *di.Scope, descriptors ...Descriptor) ([]App, error) {
	out := make([]App, 0, len(descriptors))
	for i, d := range descriptors {
		if d.Build == nil {
			return nil, fmt.Errorf("module: descriptor %d has no Build function", i)
		}
		app := d.Build(s)
		if app == nil {
			return nil, fmt.Errorf("module: descriptor %d built a nil app", i)
		}
		out = append(out, app)
	}
	return out, nil
}

// Seed runs every module's Seeder, in declaration order, stopping at the
// first failure. Call it after migrations and before serving.
func Seed(ctx context.Context, apps ...App) error {
	for _, a := range apps {
		if a == nil {
			continue
		}
		for _, m := range a.Modules() {
			s, ok := m.(Seeder)
			if !ok {
				continue
			}
			if err := s.Seed(ctx); err != nil {
				return fmt.Errorf("module %s.%s: seeding: %w", a.Name(), m.Name(), err)
			}
		}
	}
	return nil
}

// OwnedEntities maps "app.module" to the tables it declares.
//
// Two modules claiming one table is a design error that shows up as a
// mysterious write from the "wrong" service months later; this is what makes
// it checkable.
func OwnedEntities(apps ...App) map[string][]any {
	out := map[string][]any{}
	for _, a := range apps {
		if a == nil {
			continue
		}
		for _, m := range a.Modules() {
			if eo, ok := m.(EntityOwner); ok {
				if e := eo.Entities(); len(e) > 0 {
					out[a.Name()+"."+m.Name()] = e
				}
			}
		}
	}
	return out
}

func joinPath(prefix, path string) string {
	prefix = strings.TrimSuffix(prefix, "/")
	if path == "" || path == "/" {
		if prefix == "" {
			return "/"
		}
		return prefix
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return prefix + path
}
