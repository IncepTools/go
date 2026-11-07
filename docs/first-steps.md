# First steps

We will build a small catalog service: one domain, one resource, a database,
and the checks that keep it honest.

## Install

```bash
mkdir shop && cd shop
go mod init github.com/acme/shop
go get github.com/IncepTools/go
go install github.com/IncepTools/go/cmd/icpt@latest
```

Go 1.26 or newer.

## Scaffold

```bash
icpt init
icpt generate module catalog/products
```

`icpt init` writes `icpt.json`, the project configuration. `icpt generate
module` writes a complete CRUD module and the domain aggregator, and never
overwrites a file that already exists:

```
apps/catalog/
├── catalog_app.go
└── products/
    ├── products_module.go
    ├── constants/constants.go
    ├── dto/product_dto.go
    ├── entity/product.go
    ├── repository/product_repository.go
    ├── service/product_service.go
    └── controller/product_controller.go
```

Read the generated code before changing it. It already does the things that
are easy to forget: the repository is scoped, the list endpoint is paginated
with an allow-listed sort, the create route carries a guard, the update DTO
uses pointers, and the service returns typed errors rather than strings.

## Wire the domain

`apps/catalog/catalog_app.go` has two commented lines. Fill them in:

```go
// Providers registers everything this domain's modules resolve.
func Providers(c *di.Container) error {
	return errors.Join(
		products.Providers(c),
	)
}

// NewApp wires the domain's modules.
func NewApp(s *di.Scope) *App {
	return &App{BaseApp: module.NewApp("catalog", products.New(s))}
}
```

## Configuration

Declare the whole surface as a struct, so a misconfigured deployment cannot
start:

```go
// internal/appconfig/config.go
package appconfig

import "time"

type Config struct {
	Port           int           `env:"PORT" default:"8080"`
	LogLevel       string        `env:"LOG_LEVEL" default:"info"`
	LogFormat      string        `env:"LOG_FORMAT" default:"json"`
	RequestTimeout time.Duration `env:"REQUEST_TIMEOUT" default:"30s"`

	DatabaseURL string `env:"DATABASE_URL" required:"true" secret:"true"`
}
```

## main.go

```go
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	inceptools "github.com/IncepTools/go"
	"github.com/IncepTools/go/src/core/config"
	"github.com/IncepTools/go/src/core/di"
	"github.com/IncepTools/go/src/core/logx"
	"github.com/IncepTools/go/src/web/middleware"

	"github.com/acme/shop/apps/catalog"
	appconfig "github.com/acme/shop/internal/appconfig"
)

var version = "dev" // -ldflags "-X main.version=$(git describe --tags)"

func main() {
	if err := run(); err != nil {
		log.Printf("fatal: %v", err)
		os.Exit(1)
	}
}

func run() error {
	// A real environment variable always beats the file, so a committed
	// .env cannot override what a deployment sets.
	_ = config.LoadDotEnv(".env")

	var cfg appconfig.Config
	if err := config.Load(&cfg); err != nil {
		return err // every missing variable, in one message
	}

	logger := logx.New(logx.Options{
		Level:   logx.ParseLevel(cfg.LogLevel),
		Format:  logx.Format(cfg.LogFormat),
		Service: "shop",
	})
	slog.SetDefault(logger)

	db, err := gorm.Open(postgres.Open(cfg.DatabaseURL), &gorm.Config{})
	if err != nil {
		return err
	}

	app := inceptools.New(inceptools.Options{
		Service: "shop",
		Version: version,
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Prefix:  "/api/v1",
		Logger:  logger,
		Middleware: middleware.ChainOptions{
			Timeout:  cfg.RequestTimeout,
			Identity: resolveIdentity,
			AccessLog: middleware.AccessLogOptions{
				SkipPaths:     []string{"/livez", "/healthz", "/readyz"},
				SlowThreshold: time.Second,
			},
		},
	})

	di.MustSupply(app.Container, db)

	if err := app.Mount(catalog.Descriptor()); err != nil {
		return err
	}
	return app.Run(context.Background())
}
```

## Identity

`middleware.Identity` puts the account and the actor on the context. It
decides nothing — a guard does. Keeping them apart is what lets an endpoint be
public and still know who the caller is when they happen to be signed in.

```go
func resolveIdentity(c fiber.Ctx) (ctxkit.Tenant, ctxkit.Actor, error) {
	claims, err := verifyBearer(c.Get("Authorization"))
	if err != nil {
		// Anonymous. A guard on the route will refuse if it must.
		return ctxkit.Tenant{}, ctxkit.Actor{}, nil
	}
	return ctxkit.Tenant{AccountID: claims.Account},
		ctxkit.Actor{UserID: claims.User, Roles: claims.Roles}, nil
}
```

Never trust a header for identity. The bundled example does, and says so in a
comment, because an example must run with nothing else installed.

## The schema

Nothing calls `AutoMigrate`. Write the migration:

```bash
icpt create add products table
# edit migrations/000001_add_products_table.up.sql and .down.sql
icpt migrate
```

```sql
-- 000001_add_products_table.up.sql
CREATE TABLE products (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id  UUID NOT NULL,
    name        TEXT NOT NULL,
    description TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ
);
CREATE INDEX products_account_id_idx ON products (account_id);
CREATE UNIQUE INDEX products_account_name_idx ON products (account_id, name)
    WHERE deleted_at IS NULL;
```

That unique index matters even though the service also checks for a duplicate
name: the check alone is a race — two concurrent creates both see nothing and
both insert. The check gives a good message, the index gives the guarantee.
You need both.

## Production telemetry and reloads

Add one OpenTelemetry setup at the application boundary:

```go
tel, err := telemetry.New(telemetry.Config{
	ServiceName: "shop",
	SpanExporter: spanExporter,
	MetricReader: metricReader,
})
if err != nil {
	return err
}

app := inceptools.New(inceptools.Options{Service: "shop", Telemetry: tel})
app.OnReload(func(ctx context.Context) error { return cfg.Load(ctx) })
```

The framework then instruments HTTP, scheduler, queue, event, and custom
function executions and flushes providers during shutdown. A server receives
reloads through SIGHUP; a serverless adapter calls `app.Reload(ctx)`.

`module.WithEntities(&entity.Product{})` in the module file records that this
module *owns* that table. It is metadata — it answers "who owns this?" and
catches two modules claiming the same one — not a schema source.

## Run it

```bash
go run .
```

```bash
curl localhost:8080/livez     # liveness: checks nothing, by design
curl localhost:8080/healthz   # compatibility alias
curl localhost:8080/readyz     # readiness: 503 while a dependency is down
curl localhost:8080/health     # the full report, for humans

curl -H "Authorization: Bearer $TOKEN" localhost:8080/api/v1/catalog/products
```

## Keep it honest

```bash
icpt lint        # the layering rules
icpt doctor      # is this project set up correctly
go test ./...
```

And add the structural test that is worth more than its three lines:

```go
func TestEveryWriteIsGuarded(t *testing.T) {
	if un := app.Routes.Unguarded(); len(un) > 0 {
		t.Fatalf("mutating routes with no guard: %v", un)
	}
}
```

## Next

- [Modules](modules.md) — what a module declares and why there are two phases.
- [Controllers](controllers.md) — routes as values.
- [Database](database.md) — the scoped repository and transactions.
- [Testing](testing.md) — exercising the real pipeline in memory.
