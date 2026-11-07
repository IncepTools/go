package scaffold

// The templates below are the reference shape of a module. They are written
// to be read as much as to be run: a developer who opens generated code and
// copies its patterns should end up with a correct module, so the comments
// explain the decisions rather than describing the syntax.

const constantsTmpl = `package constants

// ModuleName is the route segment and the registry key for this module.
const ModuleName = "{{.Module}}"

// Messages returned to clients. They live here rather than inline so the same
// sentence is not spelled three different ways across the handlers, and so
// they can be swept when the wording changes.
const (
	MsgListed  = "{{.Entity}}s retrieved"
	MsgFetched = "{{.Entity}} retrieved"
	MsgCreated = "{{.Entity}} created"
	MsgUpdated = "{{.Entity}} updated"
	MsgDeleted = "{{.Entity}} deleted"
)

// EntityName is the noun used in error messages: "{{.Entity}} not found".
const EntityName = "{{.Module}}"

// Sortable is the allow-list of columns a client may sort by.
//
// It is an allow-list, not a filter, because the sort key is interpolated
// into the ORDER BY clause. Anything not named here is refused at the edge.
var Sortable = []string{"created_at", "name"}
`

const entityTmpl = `package entity

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// {{.Entity}} is the stored shape of a {{.Module}} record.
//
// The schema comes from a versioned migration, not from AutoMigrate. This
// struct must stay in step with it: a field whose column does not exist
// compiles, runs, and fails at query time with an error that reads like a
// driver bug.
type {{.Entity}} struct {
	ID uuid.UUID __BT__gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"__BT__

	// AccountID is the tenancy boundary. Every query against this table is
	// scoped by it, by the repository rather than by each caller.
	AccountID uuid.UUID __BT__gorm:"type:uuid;not null;index" json:"account_id"__BT__

	Name        string __BT__gorm:"type:text;not null" json:"name"__BT__
	Description string __BT__gorm:"type:text" json:"description,omitempty"__BT__

	CreatedAt time.Time      __BT__gorm:"not null;default:now()" json:"created_at"__BT__
	UpdatedAt time.Time      __BT__gorm:"not null;default:now()" json:"updated_at"__BT__
	DeletedAt gorm.DeletedAt __BT__gorm:"index" json:"-"__BT__
}

// TableName pins the table explicitly rather than relying on GORM's
// pluraliser, which disagrees with hand-written migrations often enough to
// matter.
func ({{.Entity}}) TableName() string { return "{{.Package}}" }
`

const dtoTmpl = `package dto

import "time"

// Create{{.Entity}} is the create request.
type Create{{.Entity}} struct {
	Name        string __BT__json:"name" validate:"required,min=2,max=100"__BT__
	Description string __BT__json:"description" validate:"omitempty,max=500"__BT__
}

// Update{{.Entity}} is the update request.
//
// Every field is a POINTER. With a plain string, GORM and encoding/json
// cannot tell "the client did not send this" from "the client sent an empty
// value" — so a rename silently clears the description, and a request to set
// a flag to false does nothing at all.
type Update{{.Entity}} struct {
	Name        *string __BT__json:"name" validate:"omitempty,min=2,max=100"__BT__
	Description *string __BT__json:"description" validate:"omitempty,max=500"__BT__
}

// {{.Entity}}Response is what the API returns.
//
// It is a separate type from the entity ON PURPOSE. Returning the entity
// publishes every column you ever add — including the internal ones — and
// turns a schema change into an unannounced API change.
type {{.Entity}}Response struct {
	ID          string    __BT__json:"id"__BT__
	Name        string    __BT__json:"name"__BT__
	Description string    __BT__json:"description,omitempty"__BT__
	CreatedAt   time.Time __BT__json:"created_at"__BT__
	UpdatedAt   time.Time __BT__json:"updated_at"__BT__
}
`

const repositoryTmpl = `package repository

import (
	"gorm.io/gorm"

	"{{.ImportBase}}/constants"
	"{{.ImportBase}}/entity"

	"github.com/IncepTools/go/src/data/repo"
)

// {{.Entity}}Repository is the data access for {{.Module}}.
//
// It embeds the generic repository, which applies the account scope to every
// query and refuses one with no tenant on the context. Add methods here for
// anything the generic surface does not cover; do not reach past it to the
// *gorm.DB from a service.
type {{.Entity}}Repository struct {
	*repo.Repository[entity.{{.Entity}}]
}

// New{{.Entity}}Repository builds the repository.
func New{{.Entity}}Repository(db *gorm.DB) *{{.Entity}}Repository {
	return &{{.Entity}}Repository{
		Repository: repo.New[entity.{{.Entity}}](db,
			repo.WithEntityName(constants.EntityName),
			repo.Tenanted(),
			repo.WithSortable(constants.Sortable...),
		),
	}
}
`

const serviceTmpl = `package service

import (
	"context"

	"github.com/google/uuid"

	"{{.ImportBase}}/dto"
	"{{.ImportBase}}/entity"
	"{{.ImportBase}}/repository"

	"github.com/IncepTools/go/src/core/ctxkit"
	"github.com/IncepTools/go/src/core/errs"
	"github.com/IncepTools/go/src/data/gormx"
	"github.com/IncepTools/go/src/data/repo"
	"github.com/IncepTools/go/src/web/pagination"
)

// {{.Entity}}Service holds the business rules for {{.Module}}.
//
// It knows nothing about HTTP. It takes a context and plain arguments, and
// returns a value or an *errs.Error. That is what lets the same logic serve
// an API request, a scheduled job and a CLI command without any of them
// pretending to be a request.
type {{.Entity}}Service struct {
	repo *repository.{{.Entity}}Repository
}

// New{{.Entity}}Service builds the service.
func New{{.Entity}}Service(r *repository.{{.Entity}}Repository) *{{.Entity}}Service {
	return &{{.Entity}}Service{repo: r}
}

// List returns a page of {{.Module}} for the tenant on ctx.
func ({{.Receiver}} *{{.Entity}}Service) List(ctx context.Context, q pagination.Query) (repo.Page[entity.{{.Entity}}], error) {
	return {{.Receiver}}.repo.List(ctx, q)
}

// Get returns one record, or NotFound when it does not exist or is not the
// caller's.
func ({{.Receiver}} *{{.Entity}}Service) Get(ctx context.Context, id uuid.UUID) (*entity.{{.Entity}}, error) {
	return {{.Receiver}}.repo.FindByID(ctx, id)
}

// Create stores a new record.
func ({{.Receiver}} *{{.Entity}}Service) Create(ctx context.Context, req dto.Create{{.Entity}}) (*entity.{{.Entity}}, error) {
	tenant := ctxkit.TenantOf(ctx)
	if tenant.Zero() {
		return nil, errs.Unauthorized("no account context").WithCode("NO_TENANT")
	}

	// Uniqueness is checked here for a good error message AND enforced by a
	// unique index in the migration. The check alone is a race: two
	// concurrent creates both see nothing and both insert. The index alone
	// gives the user a constraint violation instead of a sentence. You need
	// both.
	exists, err := {{.Receiver}}.repo.Exists(ctx, gormx.Where("name = ?", req.Name))
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, errs.Conflict("a {{.Module}} named %q already exists", req.Name).
			WithCode("NAME_TAKEN")
	}

	item := &entity.{{.Entity}}{
		AccountID:   tenant.AccountID,
		Name:        req.Name,
		Description: req.Description,
	}
	if err := {{.Receiver}}.repo.Create(ctx, item); err != nil {
		return nil, err
	}
	return item, nil
}

// Update applies the fields the client actually sent.
func ({{.Receiver}} *{{.Entity}}Service) Update(ctx context.Context, id uuid.UUID, req dto.Update{{.Entity}}) (*entity.{{.Entity}}, error) {
	fields := map[string]any{}
	if req.Name != nil {
		fields["name"] = *req.Name
	}
	if req.Description != nil {
		fields["description"] = *req.Description
	}
	if len(fields) == 0 {
		return nil, errs.Invalid("nothing to update").WithCode("EMPTY_UPDATE")
	}
	if err := {{.Receiver}}.repo.UpdateByID(ctx, id, fields); err != nil {
		return nil, err
	}
	return {{.Receiver}}.repo.FindByID(ctx, id)
}

// Delete removes a record.
func ({{.Receiver}} *{{.Entity}}Service) Delete(ctx context.Context, id uuid.UUID) error {
	return {{.Receiver}}.repo.DeleteByID(ctx, id)
}

// ToResponse maps the entity to the public shape.
func ToResponse(e *entity.{{.Entity}}) dto.{{.Entity}}Response {
	return dto.{{.Entity}}Response{
		ID:          e.ID.String(),
		Name:        e.Name,
		Description: e.Description,
		CreatedAt:   e.CreatedAt,
		UpdatedAt:   e.UpdatedAt,
	}
}

// ToResponses maps a page.
func ToResponses(items []entity.{{.Entity}}) []dto.{{.Entity}}Response {
	out := make([]dto.{{.Entity}}Response, 0, len(items))
	for i := range items {
		out = append(out, ToResponse(&items[i]))
	}
	return out
}
`

const controllerTmpl = `package controller

import (
	"github.com/gofiber/fiber/v3"

	"{{.ImportBase}}/constants"
	"{{.ImportBase}}/dto"
	"{{.ImportBase}}/service"

	"github.com/IncepTools/go/src/web/binding"
	"github.com/IncepTools/go/src/web/guard"
	"github.com/IncepTools/go/src/web/pagination"
	"github.com/IncepTools/go/src/web/pipe"
	"github.com/IncepTools/go/src/web/response"
	"github.com/IncepTools/go/src/web/router"
)

// {{.Entity}}Controller reads requests, calls the service and renders the
// result. It contains no business rules — if a condition belongs in a
// scheduled job as well as here, it belongs in the service.
type {{.Entity}}Controller struct {
	svc *service.{{.Entity}}Service
}

// New{{.Entity}}Controller builds the controller.
func New{{.Entity}}Controller(svc *service.{{.Entity}}Service) *{{.Entity}}Controller {
	return &{{.Entity}}Controller{svc: svc}
}

// Routes declares this controller's endpoints.
func (ctrl *{{.Entity}}Controller) Routes() []router.Route {
	return []router.Route{
		router.Get("/", ctrl.List).
			Summary("List {{.Module}}").Tag("{{.App}}").
			QueryParam("page", "Page number").
			QueryParam("limit", "Items per page").
			Returns([]dto.{{.Entity}}Response{}),

		router.Post("/", ctrl.Create).
			Guard(guard.Tenanted).
			Summary("Create a {{.Module}}").Tag("{{.App}}").
			Accepts(dto.Create{{.Entity}}{}).
			Returns(dto.{{.Entity}}Response{}, fiber.StatusCreated),

		router.Get("/:id", ctrl.Get).
			Summary("Get a {{.Module}}").Tag("{{.App}}").
			Param("id", "{{.Entity}} id").
			Returns(dto.{{.Entity}}Response{}),

		router.Patch("/:id", ctrl.Update).
			Guard(guard.Tenanted).
			Summary("Update a {{.Module}}").Tag("{{.App}}").
			Param("id", "{{.Entity}} id").
			Accepts(dto.Update{{.Entity}}{}),

		router.Delete("/:id", ctrl.Delete).
			Guard(guard.Tenanted).
			Summary("Delete a {{.Module}}").Tag("{{.App}}").
			Param("id", "{{.Entity}} id"),
	}
}

// List handles GET /.
func (ctrl *{{.Entity}}Controller) List(c fiber.Ctx) error {
	q, err := pagination.FromWith(c, pagination.Options{
		Sortable:     constants.Sortable,
		DefaultSort:  "created_at",
		DefaultOrder: pagination.Desc,
	})
	if err != nil {
		return response.Fail(c, err)
	}
	page, err := ctrl.svc.List(c.Context(), q)
	if err != nil {
		return response.Fail(c, err)
	}
	return response.List(c, constants.MsgListed, service.ToResponses(page.Items), q.Page, q.Limit, page.Total)
}

// Get handles GET /:id.
func (ctrl *{{.Entity}}Controller) Get(c fiber.Ctx) error {
	id, err := pipe.UUIDParam(c, "id")
	if err != nil {
		return response.Fail(c, err)
	}
	item, err := ctrl.svc.Get(c.Context(), id)
	if err != nil {
		return response.Fail(c, err)
	}
	return response.OK(c, constants.MsgFetched, service.ToResponse(item))
}

// Create handles POST /.
func (ctrl *{{.Entity}}Controller) Create(c fiber.Ctx) error {
	req, err := binding.Body[dto.Create{{.Entity}}](c)
	if err != nil {
		return response.Fail(c, err)
	}
	item, err := ctrl.svc.Create(c.Context(), req)
	if err != nil {
		return response.Fail(c, err)
	}
	return response.Created(c, constants.MsgCreated, service.ToResponse(item))
}

// Update handles PATCH /:id.
func (ctrl *{{.Entity}}Controller) Update(c fiber.Ctx) error {
	id, err := pipe.UUIDParam(c, "id")
	if err != nil {
		return response.Fail(c, err)
	}
	req, err := binding.Body[dto.Update{{.Entity}}](c)
	if err != nil {
		return response.Fail(c, err)
	}
	item, err := ctrl.svc.Update(c.Context(), id, req)
	if err != nil {
		return response.Fail(c, err)
	}
	return response.OK(c, constants.MsgUpdated, service.ToResponse(item))
}

// Delete handles DELETE /:id.
func (ctrl *{{.Entity}}Controller) Delete(c fiber.Ctx) error {
	id, err := pipe.UUIDParam(c, "id")
	if err != nil {
		return response.Fail(c, err)
	}
	if err := ctrl.svc.Delete(c.Context(), id); err != nil {
		return response.Fail(c, err)
	}
	return response.OK(c, constants.MsgDeleted, nil)
}
`

const moduleTmpl = `package {{.Package}}

import (
	"gorm.io/gorm"

	"{{.ImportBase}}/constants"
	"{{.ImportBase}}/controller"
	"{{.ImportBase}}/entity"
	"{{.ImportBase}}/repository"
	"{{.ImportBase}}/service"

	"github.com/IncepTools/go/src/core/di"
	"github.com/IncepTools/go/src/core/module"
	"github.com/IncepTools/go/src/web/guard"
	"github.com/IncepTools/go/src/web/router"
)

// Module is the {{.Module}} resource.
type Module struct {
	module.Base
}

// Providers registers this module's dependencies.
//
// It is a package function, not a method, because it must run BEFORE any
// module is constructed — New below resolves the service out of the
// container, so the service has to be registered first. A method could only
// run once the module existed, which is too late.
//
// Registration is separate from construction and construction is lazy, so
// another module may depend on this service regardless of which was listed
// first in the app.
func Providers(c *di.Container) error {
	if err := di.Provide(c, func(s *di.Scope) (*repository.{{.Entity}}Repository, error) {
		return repository.New{{.Entity}}Repository(di.MustResolve[*gorm.DB](s)), nil
	}); err != nil {
		return err
	}
	return di.Provide(c, func(s *di.Scope) (*service.{{.Entity}}Service, error) {
		return service.New{{.Entity}}Service(di.MustResolve[*repository.{{.Entity}}Repository](s)), nil
	})
}

// New builds the module. It resolves what it needs from the container, so
// adding a dependency later changes this function and nothing else.
func New(s *di.Scope) *Module {
	ctrl := controller.New{{.Entity}}Controller(di.MustResolve[*service.{{.Entity}}Service](s))
	return &Module{
		Base: module.NewBase(constants.ModuleName,
			module.WithControllers(ctrl),
			// Declaring the entity records who owns this table. The schema
			// itself comes from a migration.
			module.WithEntities(&entity.{{.Entity}}{}),
			// Applied to every route in the module. Per-route guards in the
			// controller add to these.
			module.WithGuards(guard.Authenticated),
		),
	}
}

// Ensure the module satisfies the contracts at compile time rather than at
// boot: a typo in a method name is a build failure here, not a route that
// quietly never mounts.
var (
	_ module.Module      = (*Module)(nil)
	_ module.EntityOwner = (*Module)(nil)
	_ router.Controller  = (*controller.{{.Entity}}Controller)(nil)
)
`

const moduleTestTmpl = `package {{.Package}}_test

import (
	"testing"

	"github.com/IncepTools/go/src/core/module"
	"github.com/IncepTools/go/src/testkit"
)

// TestRoutesAreGuarded asserts that every mutating route in this module
// carries a guard.
//
// It is the cheapest security control available: the failure it catches — a
// write endpoint shipped without authentication — is both easy to commit and
// expensive to discover in production.
func TestRoutesAreGuarded(t *testing.T) {
	t.Skip("add this module to {{.App}}.NewApp, then remove this skip")

	// c := di.New()
	// di.MustSupply(c, db)
	// _ = {{.App}}.Providers(c)
	// app := {{.App}}.NewApp(di.NewScope(c))

	var app module.App
	h := testkit.Mount(t, "/api/v1", app)
	_ = h
}
`

const appTmpl = `package {{.App}}

import (
	"errors"

	"github.com/IncepTools/go/src/core/di"
	"github.com/IncepTools/go/src/core/module"
	// "{{.GoModule}}/{{.AppsDir}}/{{.App}}/{{.Package}}"
)

// App is the {{.App}} domain.
type App struct {
	module.BaseApp
}

// Descriptor is how a service mounts this domain: register every provider
// first, then build.
//
// The two phases are a correctness requirement, not ceremony. Building a
// module constructs its controller, which resolves its service out of the
// container — so every provider in the whole application must be registered
// before any module is built.
func Descriptor() module.Descriptor {
	return module.Descriptor{
		Providers: Providers,
		Build:     func(s *di.Scope) module.App { return NewApp(s) },
	}
}

// Providers registers everything this domain's modules resolve.
func Providers(c *di.Container) error {
	return errors.Join(
	// {{.Package}}.Providers(c),
	)
}

// NewApp wires the domain's modules. Every provider is registered by the
// time this runs.
//
// Order matters only for lifecycle and shutdown — routes are matched by path
// — so list them in the order a reader would expect, not the order they were
// written.
func NewApp(s *di.Scope) *App {
	return &App{BaseApp: module.NewApp("{{.App}}",
	// {{.Package}}.New(s),
	)}
}

var _ module.App = (*App)(nil)
`

const mainTmpl = `package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/IncepTools/go"
	"github.com/IncepTools/go/src/core/config"
	"github.com/IncepTools/go/src/core/logx"
	"github.com/IncepTools/go/src/web/middleware"

	appconfig "{{.GoModule}}/internal/appconfig"
)

func main() {
	if err := run(); err != nil {
		// Log and exit non-zero. A service that cannot configure itself must
		// not come up half-working and start serving errors.
		log.Printf("fatal: %v", err)
		os.Exit(1)
	}
}

func run() error {
	_ = config.LoadDotEnv(".env")

	var cfg appconfig.Config
	if err := config.Load(&cfg); err != nil {
		return err
	}

	logger := logx.New(logx.Options{
		Level:   logx.ParseLevel(cfg.LogLevel),
		Format:  logx.Format(cfg.LogFormat),
		Service: "{{.Service}}",
	})
	slog.SetDefault(logger)

	app := inceptools.New(inceptools.Options{
		Service: "{{.Service}}",
		Version: version,
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Prefix:  "/api/v1",
		Logger:  logger,
		Middleware: middleware.ChainOptions{
			Timeout: cfg.RequestTimeout,
			AccessLog: middleware.AccessLogOptions{
				SkipPaths:     []string{"/livez", "/healthz", "/readyz"},
				SlowThreshold: time.Second,
			},
		},
	})

	// Optional OpenTelemetry setup: create tel before inceptools.New and add
	// Telemetry: tel to the Options below. Exporters are intentionally selected
	// by the service rather than by generated code.

	// Supply the dependencies built outside the container, then mount the
	// domains:
	//
	//   di.MustSupply(app.Container, db)
	//   if err := app.Mount(catalog.Descriptor()); err != nil { return err }

	return app.Run(context.Background())
}

// version is set at build time: -ldflags "-X main.version=$(git describe --tags)".
var version = "dev"
`

const serviceConfigTmpl = `// Package appconfig is the whole configuration surface of {{.Service}}.
//
// Declaring it in one struct means a misconfigured deployment fails at boot
// with a list of what is missing, rather than at 2am with the first request
// that needed the variable nobody set.
package appconfig

import "time"

// Config is loaded once, at start-up.
type Config struct {
	Port           int           __BT__env:"PORT" default:"8080"__BT__
	LogLevel       string        __BT__env:"LOG_LEVEL" default:"info"__BT__
	LogFormat      string        __BT__env:"LOG_FORMAT" default:"json"__BT__
	RequestTimeout time.Duration __BT__env:"REQUEST_TIMEOUT" default:"30s"__BT__

	DatabaseURL string __BT__env:"DATABASE_URL" required:"true" secret:"true"__BT__
	RedisURL    string __BT__env:"REDIS_URL"__BT__
}
`

const envTmpl = `# Copy to .env. A real environment variable always wins over this file, so a
# committed .env cannot override what a deployment sets.
PORT=8080
LOG_LEVEL=debug
LOG_FORMAT=text
REQUEST_TIMEOUT=30s

DATABASE_URL=postgres://postgres:postgres@localhost:5432/{{.Service}}?sslmode=disable
REDIS_URL=redis://localhost:6379/0
`

const makefileTmpl = `.PHONY: run build test lint tidy

run:
	go run .

build:
	go build -o bin/{{.Service}} .

test:
	go test ./...

# Structure rules: layering, module layout, cross-module coupling.
lint:
	go vet ./...
	icpt lint

tidy:
	go mod tidy
`

const serviceReadmeTmpl = `# {{.Service}}

Built with [inceptools](https://github.com/IncepTools/go).

## Layout

    apps/<domain>/<module>/
        <module>_module.go    module declaration: name, controllers, entities, guards
        constants/            module name, messages, sortable columns
        dto/                  request and response shapes
        entity/               stored shapes
        repository/           data access
        service/              business rules — no HTTP
        controller/           request handling — no SQL

## Commands

    make run       start the service
    make test      run the tests
    make lint      vet plus the structure rules
`
