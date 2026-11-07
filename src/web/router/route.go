// Package router makes routes DATA.
//
// The usual way of registering routes — calling r.Get(...) in a Register
// function — throws away everything about them the moment it runs. Nothing
// can tell you afterwards which routes exist, which are unguarded, or what
// they do, because the registration was a side effect rather than a value.
//
// So a controller here RETURNS its routes instead of registering them. The
// same value that mounts the route also answers `icpt routes`, generates the
// OpenAPI document, and lets a structural test assert that every mutating
// route in the tree carries an authentication guard. That last one is the
// reason this design is worth the small ceremony: a missing guard becomes a
// failing test rather than an incident.
//
//	func (c *ProductController) Routes() []router.Route {
//	    return []router.Route{
//	        router.Get("/", c.List).Summary("List products").Tag("catalog"),
//	        router.Post("/", c.Create).Guard(guard.Tenanted).Summary("Create a product"),
//	        router.Get("/:id", c.Get).Param("id", "Product id"),
//	    }
//	}
package router

import (
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/IncepTools/go/src/web/guard"
	"github.com/IncepTools/go/src/web/interceptor"
)

// ParamDoc documents a path, query or header parameter for the generated
// OpenAPI document.
type ParamDoc struct {
	Name        string `json:"name"`
	In          string `json:"in"` // path | query | header
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Example     string `json:"example,omitempty"`
}

// Doc is everything about a route that is for humans and generators rather
// than for the router: the OpenAPI surface.
//
// It is a nested struct rather than fields on Route so that the fluent
// builders below can carry the obvious names — Summary, Tags, Response — and
// still be methods.
type Doc struct {
	OperationID string     `json:"operationId,omitempty"`
	Summary     string     `json:"summary,omitempty"`
	Description string     `json:"description,omitempty"`
	Tags        []string   `json:"tags,omitempty"`
	Params      []ParamDoc `json:"params,omitempty"`
	// RequestBody and Response are sample values (usually a zero DTO) that
	// the OpenAPI generator reflects over.
	RequestBody any `json:"-"`
	Response    any `json:"-"`
	// Statuses the route can return beyond the success one.
	Statuses   []int `json:"statuses,omitempty"`
	Deprecated bool  `json:"deprecated,omitempty"`
}

// Route is one HTTP endpoint, with everything known about it in one value.
type Route struct {
	Method  string
	Path    string
	Handler fiber.Handler

	// Guards run before the handler, in order.
	Guards []guard.Guard
	// Interceptors wrap the handler; the first is outermost.
	Interceptors []interceptor.Interceptor
	// Middleware is an escape hatch for Fiber handlers that are neither a
	// guard nor an interceptor — a body limit, a websocket upgrade.
	Middleware []fiber.Handler

	// Doc carries the documentation surface.
	Doc Doc

	// Public marks a route as deliberately unauthenticated. Structural tests
	// use it to tell "no guard because it is public" apart from "no guard
	// because somebody forgot", which otherwise look identical.
	Public bool
}

// New builds a route.
func New(method, path string, h fiber.Handler) Route {
	return Route{Method: strings.ToUpper(method), Path: normalise(path), Handler: h}
}

// Get builds a GET route.
func Get(path string, h fiber.Handler) Route { return New(http.MethodGet, path, h) }

// Post builds a POST route.
func Post(path string, h fiber.Handler) Route { return New(http.MethodPost, path, h) }

// Put builds a PUT route.
func Put(path string, h fiber.Handler) Route { return New(http.MethodPut, path, h) }

// Patch builds a PATCH route.
func Patch(path string, h fiber.Handler) Route { return New(http.MethodPatch, path, h) }

// Delete builds a DELETE route.
func Delete(path string, h fiber.Handler) Route { return New(http.MethodDelete, path, h) }

// Head builds a HEAD route.
func Head(path string, h fiber.Handler) Route { return New(http.MethodHead, path, h) }

// Options builds an OPTIONS route.
func Options(path string, h fiber.Handler) Route { return New(http.MethodOptions, path, h) }

// Guard appends guards, in execution order.
func (r Route) Guard(g ...guard.Guard) Route {
	r.Guards = append(append([]guard.Guard(nil), r.Guards...), g...)
	return r
}

// Intercept appends interceptors; the first added is the outermost.
func (r Route) Intercept(i ...interceptor.Interceptor) Route {
	r.Interceptors = append(append([]interceptor.Interceptor(nil), r.Interceptors...), i...)
	return r
}

// Use appends raw Fiber middleware.
func (r Route) Use(h ...fiber.Handler) Route {
	r.Middleware = append(append([]fiber.Handler(nil), r.Middleware...), h...)
	return r
}

// ID sets the OpenAPI operationId.
func (r Route) ID(id string) Route { r.Doc.OperationID = id; return r }

// Summary sets the one-line description.
func (r Route) Summary(s string) Route { r.Doc.Summary = s; return r }

// Describe sets the long description.
func (r Route) Describe(s string) Route { r.Doc.Description = s; return r }

// Tag appends OpenAPI tags.
func (r Route) Tag(t ...string) Route {
	r.Doc.Tags = append(append([]string(nil), r.Doc.Tags...), t...)
	return r
}

// Param documents a required path parameter.
func (r Route) Param(name, description string) Route {
	return r.withParam(ParamDoc{Name: name, In: "path", Description: description, Required: true})
}

// QueryParam documents an optional query parameter.
func (r Route) QueryParam(name, description string) Route {
	return r.withParam(ParamDoc{Name: name, In: "query", Description: description})
}

// HeaderParam documents a request header.
func (r Route) HeaderParam(name, description string) Route {
	return r.withParam(ParamDoc{Name: name, In: "header", Description: description})
}

func (r Route) withParam(p ParamDoc) Route {
	r.Doc.Params = append(append([]ParamDoc(nil), r.Doc.Params...), p)
	return r
}

// Accepts records the request body type, usually a zero DTO.
func (r Route) Accepts(sample any) Route { r.Doc.RequestBody = sample; return r }

// Returns records the success payload type and any extra status codes.
func (r Route) Returns(sample any, statuses ...int) Route {
	r.Doc.Response = sample
	r.Doc.Statuses = append(append([]int(nil), r.Doc.Statuses...), statuses...)
	return r
}

// Deprecate marks the route deprecated in the document.
func (r Route) Deprecate() Route { r.Doc.Deprecated = true; return r }

// AsPublic marks the route as intentionally unauthenticated.
func (r Route) AsPublic() Route { r.Public = true; return r }

// Controller exposes a set of routes. A controller holds no state beyond its
// service dependency and contains no business logic: it reads the request,
// calls a service, renders the result.
type Controller interface {
	Routes() []Route
}

// ControllerFunc adapts a plain function to Controller.
type ControllerFunc func() []Route

// Routes implements Controller.
func (f ControllerFunc) Routes() []Route { return f() }

func normalise(path string) string {
	if path == "" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		return "/" + path
	}
	return path
}
