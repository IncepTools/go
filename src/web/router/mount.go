package router

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/gofiber/fiber/v3"

	"github.com/IncepTools/go/src/web/guard"
	"github.com/IncepTools/go/src/web/interceptor"
)

// Mounted is a route after mounting: the declared metadata plus the full path
// it actually answers on.
type Mounted struct {
	Route
	// FullPath includes every group prefix — this is what a client calls.
	FullPath string
	// Module is the dotted owner, "catalog.products".
	Module string
	// GuardNames is flattened for printing and for assertions.
	GuardNames []string
}

// RouteJSON is the data-only representation used by route dumps.
type RouteJSON struct {
	Method     string   `json:"method"`
	Path       string   `json:"path"`
	FullPath   string   `json:"full_path"`
	Module     string   `json:"module"`
	GuardNames []string `json:"guards"`
	Public     bool     `json:"public"`
}

// JSONRoutes converts mounted routes into a serializable data-only view.
func JSONRoutes(routes []Mounted) []RouteJSON {
	serialized := make([]RouteJSON, 0, len(routes))
	for _, route := range routes {
		serialized = append(serialized, RouteJSON{
			Method: route.Method, Path: route.Path, FullPath: route.FullPath,
			Module: route.Module, GuardNames: route.GuardNames, Public: route.Public,
		})
	}
	return serialized
}

// Registry records every mounted route.
//
// It is what makes the route table introspectable: `icpt routes` prints it,
// the OpenAPI generator walks it, and a structural test asserts over it. The
// zero value is ready to use.
type Registry struct {
	mu     sync.Mutex
	routes []Mounted
}

// Add records a mounted route.
func (reg *Registry) Add(m Mounted) {
	if reg == nil {
		return
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	reg.routes = append(reg.routes, m)
}

// All returns every mounted route, sorted by path then method, so the output
// is stable across runs — a route table that reorders itself cannot be
// diffed, and a diffable route table catches an accidental route change in
// code review.
func (reg *Registry) All() []Mounted {
	if reg == nil {
		return nil
	}
	reg.mu.Lock()
	out := append([]Mounted(nil), reg.routes...)
	reg.mu.Unlock()
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].FullPath != out[j].FullPath {
			return out[i].FullPath < out[j].FullPath
		}
		return out[i].Method < out[j].Method
	})
	return out
}

// Unguarded returns every mutating route (POST, PUT, PATCH, DELETE) that
// carries neither a guard nor an explicit Public marker.
//
// Assert that this is empty in a test. It is the cheapest security control in
// the framework: the failure it catches — a new write endpoint shipped without
// authentication — is both common and expensive.
func (reg *Registry) Unguarded() []Mounted {
	var out []Mounted
	for _, m := range reg.All() {
		switch m.Method {
		case fiber.MethodPost, fiber.MethodPut, fiber.MethodPatch, fiber.MethodDelete:
			if len(m.Guards) == 0 && !m.Public {
				out = append(out, m)
			}
		}
	}
	return out
}

// Table renders the registry as aligned text for a CLI.
func (reg *Registry) Table() string {
	rows := reg.All()
	var b strings.Builder
	width := 6
	for _, r := range rows {
		width = max(width, len(r.FullPath))
	}
	fmt.Fprintf(&b, "%-7s %-*s  %-20s %s\n", "METHOD", width, "PATH", "MODULE", "GUARDS")
	for _, r := range rows {
		guards := strings.Join(r.GuardNames, ",")
		if guards == "" {
			guards = "-"
			if r.Public {
				guards = "(public)"
			}
		}
		fmt.Fprintf(&b, "%-7s %-*s  %-20s %s\n", r.Method, width, r.FullPath, r.Module, guards)
	}
	return b.String()
}

// MountOptions configures Mount.
type MountOptions struct {
	// Module names the owner in the registry, e.g. "catalog.products".
	Module string
	// Prefix is the path already applied to r by the caller's Group calls.
	// Mount cannot read it back off a fiber.Router, so pass it here or the
	// recorded FullPath will be wrong.
	Prefix string
	// Registry records the mounted routes. Optional.
	Registry *Registry
	// Guards apply to every route mounted in this call, before the route's
	// own. A module-wide authentication guard belongs here.
	Guards []guard.Guard
	// Interceptors apply to every route, outside the route's own.
	Interceptors []interceptor.Interceptor
}

// Mount registers routes on a Fiber router and records them.
//
// Per-route guards are attached as handlers on that route alone, rather than
// with r.Use: a Use on a group applies to every route after it, which means
// adding a public route below a protected one silently exposes it. Attaching
// per route makes each line say exactly what protects it.
func Mount(r fiber.Router, routes []Route, opt MountOptions) error {
	for _, rt := range routes {
		if rt.Handler == nil {
			return fmt.Errorf("router: %s %s in module %q has a nil handler",
				rt.Method, rt.Path, opt.Module)
		}
		if rt.Method == "" {
			return fmt.Errorf("router: route %s in module %q has no method", rt.Path, opt.Module)
		}

		guards := append(append([]guard.Guard(nil), opt.Guards...), rt.Guards...)
		intercepts := append(append([]interceptor.Interceptor(nil), opt.Interceptors...), rt.Interceptors...)

		// The chain runs in the order it is passed to Fiber, and the HANDLER
		// goes last. Passing the guards as the trailing arguments puts them
		// after the handler, where they can refuse a request that has already
		// been served — which is no protection at all.
		chain := make([]fiber.Handler, 0, len(rt.Middleware)+3)
		chain = append(chain, rt.Middleware...)
		if len(guards) > 0 {
			chain = append(chain, guard.Use(guards...))
		}
		if len(intercepts) > 0 {
			chain = append(chain, interceptor.Use(intercepts...))
		}
		chain = append(chain, rt.Handler)

		rest := make([]any, 0, len(chain)-1)
		for _, h := range chain[1:] {
			rest = append(rest, h)
		}
		r.Add([]string{rt.Method}, rt.Path, chain[0], rest...)

		names := make([]string, 0, len(guards))
		for _, g := range guards {
			names = append(names, g.Name())
		}
		opt.Registry.Add(Mounted{
			Route:      rt,
			FullPath:   joinPath(opt.Prefix, rt.Path),
			Module:     opt.Module,
			GuardNames: names,
		})
	}
	return nil
}

// MountController mounts everything a controller declares.
func MountController(r fiber.Router, c Controller, opt MountOptions) error {
	if c == nil {
		return nil
	}
	return Mount(r, c.Routes(), opt)
}

func joinPath(prefix, path string) string {
	prefix = strings.TrimSuffix(prefix, "/")
	switch {
	case path == "" || path == "/":
		if prefix == "" {
			return "/"
		}
		return prefix
	case !strings.HasPrefix(path, "/"):
		return prefix + "/" + path
	default:
		return prefix + path
	}
}
