<div align="center">

# inceptools

**An opinionated application framework for Go services.**

Modules, dependency injection, guards, interceptors, typed errors, structured
observability, a scoped data layer, scheduling, caching, locks, idempotency, an outbox — and a CLI that
scaffolds all of it, migrates your database, and then checks you kept to it.

[![ci](https://github.com/IncepTools/go/actions/workflows/ci.yml/badge.svg)](https://github.com/IncepTools/go/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/IncepTools/go.svg)](https://pkg.go.dev/github.com/IncepTools/go)
[![Go Report Card](https://goreportcard.com/badge/github.com/IncepTools/go)](https://goreportcard.com/report/github.com/IncepTools/go)
[![Go 1.26+](https://img.shields.io/badge/go-1.26%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/dl/)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

[Documentation](docs/) · [First steps](docs/first-steps.md) · [Serverless](docs/serverless.md) · [Examples](examples/) · [Roadmap](ROADMAP.md)

</div>

## In one minute

```go
telemetry, err := telemetry.New(telemetry.Config{
    ServiceName: "catalog",
})
if err != nil { log.Fatal(err) }

app := inceptools.New(inceptools.Options{
    Service: "catalog",
    Prefix:  "/api/v1",
    Telemetry: telemetry,
    Middleware: middleware.ChainOptions{
        AccessLog: middleware.AccessLogOptions{SlowThreshold: time.Second},
    },
})
```

Every request, scheduled job, queue attempt, and event handler receives an
OpenTelemetry span. Metrics are emitted through the configured OTel meter
provider, errors and panics are recorded, and providers flush during graceful
shutdown. Use `telemetry.Run` or `telemetry.Start` for any other function.

Set the standard `OTEL_EXPORTER_OTLP_ENDPOINT` and optional
`OTEL_EXPORTER_OTLP_HEADERS` environment variables to send traces and metrics
to SigNoz, an OpenTelemetry Collector, Elastic APM, Jaeger, Grafana, Honeycomb,
or another OTLP backend. Custom SDK providers and exporters remain injectable.
See [OpenTelemetry](docs/telemetry.md).

---

## Why

Go gives you a standard library and gets out of the way. That is the right
default for a program and the wrong one for the twentieth service in an
organisation, because by then every team has answered the same questions
differently: where business logic lives, what an error looks like on the wire,
who applies the ownership filter on a query, whether a scheduled job runs on
one replica or all of them.

inceptools answers them once. Interfaces and functions, not annotations and
reflection; nothing resolved by magic at runtime; nothing that fails at 2am
that could have failed at boot.

| | |
|---|---|
| **An ownership filter you cannot forget** | A scoped repository applies it and *refuses* a query with no account on the context. Remembering `.Where("account_id = ?")` in every query fails silently — by returning another customer's rows. |
| **One response shape, one leak check** | Every reply goes through one funnel, so "did a driver error just reach a client" is checked once rather than at four hundred call sites. |
| **Routes as data** | A controller returns its routes. The same value mounts them, prints them, documents them — and lets a test assert that every mutating route has a guard. |
| **Boot-time failure** | Config is a struct loaded once; providers are constructed eagerly before the socket opens. A missing environment variable fails the deploy, not the first request that needed it. |
| **Shutdown that drains** | Lifecycle hooks run in reverse order under a drain deadline, on a context that is *not* the cancelled one — the mistake that makes every shutdown hook skip its work. |
| **Scheduled work safe to deploy** | Jobs are singleton across replicas by a distributed lock, and a slow run skips rather than piling up behind itself. |
| **Migrations that refuse to lie** | Duplicate versions rejected, an advisory lock around the run, drift detected, rollback from the stored script. Five SQL dialects. |

## Install

**The library**

```bash
go get github.com/IncepTools/go
```

**The CLI**

```bash
go install github.com/IncepTools/go/cmd/icpt@latest   # with a Go toolchain
brew install IncepTools/tap/icpt                      # macOS and Linux
curl -fsSL https://raw.githubusercontent.com/IncepTools/go/main/install.sh | sh
```

**AI agent setup**

No vendor-specific setup is required when working in this repository. Codex,
Cursor, Claude, Gemini/Antigravity-style agents, and other agents that support
repository instructions discover the checked-in guidance automatically:

- `AGENTS.md` — shared project rules for Codex and compatible agents
- `CLAUDE.md` — Claude Code discovery bridge
- `.cursor/rules/inceptools.mdc` — Cursor project rule
- `GEMINI.md` — Gemini/Antigravity discovery bridge
- `.github/copilot-instructions.md` — GitHub Copilot discovery bridge
- `.agents/skills/inceptools/SKILL.md` — detailed canonical framework skill

Grok and agents without a documented project-file convention should start with
`AGENTS.md` and then load `.agents/skills/inceptools/SKILL.md` when the task
needs framework-specific guidance.

For another inceptools project, install the canonical skill once:

```bash
mkdir -p .agents/skills/inceptools
curl -fsSL https://raw.githubusercontent.com/IncepTools/go/main/.agents/skills/inceptools/SKILL.md \
  -o .agents/skills/inceptools/SKILL.md
```

Or copy it with Git:

```bash
git clone --depth 1 https://github.com/IncepTools/go.git /tmp/inceptools-skill
mkdir -p .agents/skills/inceptools
cp /tmp/inceptools-skill/.agents/skills/inceptools/SKILL.md .agents/skills/inceptools/
rm -rf /tmp/inceptools-skill
```

AI coding agents that support repository skills can then load
`.agents/skills/inceptools/SKILL.md` for the framework architecture, CLI,
feature patterns, testing, security, and release workflow.

Go 1.26+ (the floor Fiber v3 sets). HTTP is [Fiber v3](https://gofiber.io);
persistence is [GORM](https://gorm.io), used only by `src/data` — the rest of
the framework does not know your database exists.

The CLI links five SQL drivers, but they live under `internal/` where nothing
outside this module can reach them. A program importing one package of the
library compiles one package: `go list -deps` on a binary that imports
`src/core/errs` shows exactly that and nothing more.

## Sixty seconds

```bash
mkdir shop && cd shop
go mod init github.com/acme/shop
go get github.com/IncepTools/go

icpt init                              # writes icpt.json
icpt generate module catalog/products  # a full CRUD module, wired
go build ./...
```

You now have:

```
apps/catalog/
├── catalog_app.go               the domain: register providers, then build
└── products/
    ├── products_module.go       name, controllers, entities, guards
    ├── constants/               module name, messages, sortable columns
    ├── dto/                     requests and responses — pointers on updates
    ├── entity/                  the stored shape
    ├── repository/              scoped data access
    ├── service/                 business rules, no HTTP
    └── controller/              request handling, no SQL
```

And `main.go`:

```go
app := inceptools.New(inceptools.Options{
    Service: "shop",
    Addr:    ":8080",
    Prefix:  "/api/v1",
    Middleware: middleware.ChainOptions{
        Timeout:  15 * time.Second,
        Identity: auth.FromBearerToken,
    },
})

di.MustSupply(app.Container, db)

if err := app.Mount(catalog.Descriptor()); err != nil {
    return err
}
return app.Run(context.Background())
```

That gives you `/livez`, `/healthz` (compatibility alias), `/readyz`, and
`/health`, a request id on every response, OpenTelemetry HTTP spans and
metrics, recovered panics, access logging, graceful shutdown, SIGHUP config
reloads, and `GET|POST|PATCH|DELETE /api/v1/catalog/products`.

The same `Application` implements `net/http.Handler`, so a server runs it with
`app.Run(ctx)` while Lambda or another serverless adapter can pass `app`
directly to its HTTP bridge. Serverless runtimes can call `app.Reload(ctx)`
because they do not have process signals.

## Examples

Ten runnable services, each needing nothing installed:

```bash
cd examples
go run ./minimal    # :8080  the floor — one domain, one route, ~60 lines
go run ./todo       # :8080  in-memory CRUD, events, a scheduled purge
go run ./blog       # :8081  SQLite: migrations, transactions, an outbox
go run ./worker     # :8082  no API at all — schedules and a drain
go run ./webhooks     # :8084  signed webhook verification
go run ./queues       # :8085  retrying workers
go run ./scheduling   # :8086  singleton scheduled jobs
go run ./sse          # :8087  server-sent events
go run ./event-emitter # :8088 typed in-process events
go run ./serverless    # :8089 the same app as an http.Handler
```

[`examples/`](examples/) explains what each one demonstrates and what to try
against it. [`blog`](examples/blog) is the one to read if you want to see the
data layer; [`worker`](examples/worker) shows that a module does not have to
serve routes. The dedicated [`webhooks`](examples/webhooks),
[`queues`](examples/queues), [`scheduling`](examples/scheduling), [`sse`](examples/sse)
and [`event-emitter`](examples/event-emitter) examples each focus on one
feature. [`serverless`](examples/serverless) hosts the identical application
through `http.Handler` — `Prepare` instead of `Run`, `Reload` instead of
`SIGHUP` — which is the path a Lambda adapter takes.

## First-party capabilities

| What you need | Inceptools |
|---|---|
| Application architecture | `core/module`, `core/di`, `core/lifecycle` |
| HTTP routes and policy | `web/router`, `web/guard`, `web/interceptor`, `web/middleware` |
| Observability | `core/telemetry` — OpenTelemetry traces, metrics, propagation, and lifecycle flush |
| Server and serverless | `Application.Run`, `Application.ServeHTTP`, `Application.Reload` |
| Validation and response safety | `web/binding`, `web/pipe`, `web/response`, `web/filter` |
| Database access | `data/repo`, `data/gormx`, migrations through `icpt` |
| Recurring work and coordination | `platform/scheduler`, `platform/lock`, `platform/queue` |
| Events and streaming | `platform/events`, `web/sse` |
| External delivery | `web/webhook`, `reliability/outbox` |
| Failure handling | `reliability/retry`, `reliability/breaker`, `reliability/idempotency` |
| Project tooling | `icpt init`, `generate`, `lint`, `routes`, `doctor`, `migrate` |

## The shape

Three levels, and no more:

```
App       a bounded domain       catalog, billing, identity
Module    one resource in it     products, invoices, sessions
Layer     controller / service / repository / dto / entity
```

A module owns exactly one resource and declares what it provides, routes, owns
and needs checked. Nothing outside it reaches into its service or its entities.
`icpt lint` enforces that, and every finding says what breaks — not just which
rule it broke.

### Routes are values

```go
func (c *ProductController) Routes() []router.Route {
    return []router.Route{
        router.Get("/", c.list).
            Summary("List products").
            QueryParam("page", "Page number"),

        router.Post("/", c.create).
            Guard(guard.Tenanted).
            Accepts(dto.CreateProduct{}).
            Returns(dto.ProductResponse{}, fiber.StatusCreated),

        router.Delete("/:id", c.remove).
            Guard(guard.Tenanted, guard.HasRole("catalog:write")),
    }
}
```

Because a route is a value rather than a side effect, this works:

```go
func TestEveryWriteIsGuarded(t *testing.T) {
    if un := app.Routes.Unguarded(); len(un) > 0 {
        t.Fatalf("mutating routes with no guard: %v", un)
    }
}
```

That is the cheapest security control in the framework. The failure it catches
— a new write endpoint shipped without authentication — is both easy to commit
and expensive to find.

### Errors know their own status

A service never imports `net/http`:

```go
func (s *ProductService) Get(ctx context.Context, id uuid.UUID) (*entity.Product, error) {
    product, err := s.repo.FindByID(ctx, id)   // NotFound, already scoped
    if err != nil {
        return nil, err
    }
    if product.Archived {
        return nil, errs.Conflict("this product is archived").WithCode("PRODUCT_ARCHIVED")
    }
    return product, nil
}
```

The controller renders it, the exception filter catches anything that escaped,
and the funnel decides what a client may see:

```json
{
  "data": null,
  "meta": {
    "success": false,
    "statusCode": 409,
    "code": "PRODUCT_ARCHIVED",
    "message": "this product is archived",
    "traceId": "0b2f…",
    "route": "/api/v1/catalog/products/9c1e…"
  }
}
```

An error that is *not* an `*errs.Error` becomes a generic 500 and the original
goes to the log — because an unclassified error is by definition one nobody
checked for leaks.

### The ownership filter is not yours to remember

```go
repo.New[entity.Product](db,
    repo.WithEntityName("product"),
    repo.Tenanted(),                       // scoped on account_id
    repo.WithSortable("created_at", "price"),
)
```

Every read is filtered. Every create takes the account from the context, not
the payload. A request with no account is an error, never "all accounts".
Reaching wider is possible and has to be spelled out:

```go
r.Unscoped(ctx)   // a nightly report across every account. Deliberate, visible.
```

`UpdateFields` refuses an empty condition, because an update with no `WHERE`
rewrites every row the account owns. And it takes a map rather than a struct,
because GORM skips zero-valued struct fields — so a struct update can never set
a column to `false`, and the PATCH that disables something appears to succeed
while changing nothing.

### Validation says which field

```go
req, err := binding.Body[dto.CreateProduct](c)
if err != nil {
    return response.Fail(c, err)   // 400 with per-field detail
}
```

```json
"errors": [
  { "field": "price",     "message": "price must be 0 or more",    "rule": "gte" },
  { "field": "tags[0]",   "message": "tags[0] is required",        "rule": "required" }
]
```

Field names are the JSON ones the client sent. A DTO can implement
`Normalize()`, `SetDefaults()` and `Validate()` for cross-field rules; they run
in that order, so a default can satisfy `required`, and a cross-field rule can
assume the individual fields are already well-formed.

### Work that outlives the request

```go
events.On(bus, "audit", events.Async, func(ctx context.Context, e OrderPlaced) error {
    return audit.Record(ctx, e)
})
```

Async handlers get a **detached** context: it keeps the trace id and the
account and drops the cancellation. Handing a handler the request context means
the audit write is cancelled the moment the client disconnects — and nothing
tells you.

When the effect must survive a crash, write it in the same transaction as the
state change:

```go
return repo.Transaction(ctx, db, func(ctx context.Context) error {
    if err := r.Create(ctx, order); err != nil {
        return err
    }
    return outbox.Enqueue(ctx, db, "order.placed", order)
})
```

`Enqueue` *refuses* to run outside a transaction. Outside one it is just a
slower publish with the same hole in the middle.

### Scheduled jobs

```go
sched.MustAdd(scheduler.Job{
    Name:      "expire-abandoned-carts",
    Cron:      "0 3 * * *",
    Singleton: true,              // one replica, via the distributed lock
    Timeout:   10 * time.Minute,
    Jitter:    30 * time.Second,
    Run:       svc.ExpireAbandoned,
})
```

A malformed cron expression fails the boot rather than the night it was meant
to run. A run still going when the next is due is skipped, not queued —
queueing a slow job behind itself turns a slowdown into an unbounded backlog.

## The CLI

`icpt` reads `icpt.json` from the repository root, so it fits your layout
rather than the other way round. Comments and trailing commas are accepted,
because a file you are told to record decisions in has to allow a comment
next to the decision.

```bash
icpt init                               # write icpt.json
icpt new <service>                      # a whole service skeleton
icpt generate module <app>/<module>     # a CRUD module, wired
icpt generate app <app>                 # just the domain aggregator
icpt lint [-strict]                     # the conventions, enforced
icpt routes [-unguarded]                # the route table, from a dump
icpt doctor                             # is this project set up correctly

icpt create <name>                      # next migration pair
icpt migrate [-db <name>] [-n]          # apply what is pending
icpt status [-db <name>]                # applied, pending, drifted, missing
icpt down -steps <n> [-db <name>]       # reverse, with confirmation
```

Nothing destructive happens without `-y` or a typed confirmation, `-n` is a dry
run everywhere, and the generator never overwrites a file.

### `icpt lint`

```
error service-imports-http   apps/catalog/products/service/product_service.go:9
        a service imports github.com/gofiber/fiber/v3
        why: a service that speaks HTTP cannot be called from a scheduled job, a CLI
        or a queue consumer without faking a request. Return an *errs.Error and let
        the controller decide the status.
```

The rules: services free of HTTP, controllers free of SQL, DTOs free of the
database, entities free of both, and modules not reaching into each other's
internals. Severities and directory names are yours to set.

### `icpt migrate`

Versioned SQL in `<version>_<name>.up.sql` / `.down.sql` pairs, against
**PostgreSQL, MySQL, SQLite, SQL Server or ClickHouse**:

```jsonc
{
  "dialect": "postgres",
  "databases": {
    "primary":   { "env_variable": "DATABASE_URL",  "migration_path": "./migrations" },
    "reporting": { "env_variable": "REPORTING_URL", "dialect": "clickhouse" }
  }
}
```

The DSN lives in the environment and never in the file, because a connection
string holds a password.

What it insists on:

- **Duplicate versions are refused.** Given `000027_a` and `000027_b`, a ledger
  keyed on the version applies one and records the other's name. Nothing warns,
  and the migration that did not run simply does not exist.
- **One transaction per migration, ledger row included** — so a failure leaves
  neither the schema change nor the record, and a re-run is a clean retry. On
  MySQL and ClickHouse, where DDL commits implicitly and that is impossible, it
  writes a `running` row first and says so in the output.
- **An advisory lock around the run.** Two deploys rolling at once otherwise
  both see version 11 as current and both apply 12.
- **Rollback runs the stored script**, not the file. The database was built by
  what ran then; the file belongs to what is checked out now.
- **Drift is reported and blocks.** An applied migration whose file has since
  been edited means the ledger no longer describes a database this checkout can
  rebuild.

## Repository layout

```
src/            all library code
tests/          every test, mirroring src/ and internal/
internal/       the CLI's own packages — unreachable from outside
cmd/icpt/       the CLI
examples/       nine runnable services — a separate Go module
docs/           one page per capability
inceptools.go   the application factory, at the module root
```

`src/` is unusual for Go. It is here because this framework's argument is that
a predictable layout is worth more than the freedom to put things anywhere,
and a repository that did not follow its own advice would be hard to take
seriously. See [CONTRIBUTING.md](CONTRIBUTING.md) for the rest, including why
examples sit beside their package while tests do not.

## Documentation

The complete capability index is in [docs/README.md](docs/README.md). For
documentation tooling and assistants, [docs/llms.txt](docs/llms.txt) is the
compact canonical map. Repository maintenance rules live in
[AGENTS.md](AGENTS.md) and [CONTRIBUTING.md](CONTRIBUTING.md).

**Overview** — [Introduction](docs/introduction.md) ·
[First steps](docs/first-steps.md) ·
[Modules](docs/modules.md) ·
[Controllers](docs/controllers.md) ·
[Providers](docs/providers.md) ·
[Middleware](docs/middleware.md) ·
[Guards](docs/guards.md) ·
[Interceptors](docs/interceptors.md) ·
[Pipes](docs/pipes.md) ·
[Exception filters](docs/exception-filters.md)

**Fundamentals** — [Configuration](docs/configuration.md) ·
[Validation](docs/validation.md) ·
[Logging](docs/logging.md) ·
[Lifecycle](docs/lifecycle.md) ·
[Testing](docs/testing.md) ·
[Architecture](docs/architecture.md)

**Data** — [Database](docs/database.md) ·
[Pagination](docs/pagination.md) ·
[Migrations](docs/migrations.md)

**Techniques** — [Caching](docs/caching.md) ·
[Events](docs/events.md) ·
[Task scheduling](docs/task-scheduling.md) ·
[Queues](docs/queues.md) ·
[Webhooks](docs/webhooks.md) ·
[SSE](docs/sse.md) ·
[Distributed locks](docs/locks.md) ·
[Idempotency](docs/idempotency.md) ·
[Transactional outbox](docs/outbox.md) ·
[Resilience](docs/resilience.md) ·
[Health checks](docs/health-checks.md) ·
[Security](docs/security.md)

**Tooling** — [CLI](docs/cli.md) · [`icpt.json`](docs/project-config.md)

Every package also carries a doc comment explaining what it is for and which
failure it prevents. The separate [`examples/`](examples/) module contains
nine runnable services, while package-level examples are rendered on
[pkg.go.dev](https://pkg.go.dev/github.com/IncepTools/go).

## Packages

| Package | What it is for |
|---|---|
| [`inceptools`](.) | The application factory. Assembles, serves, drains. |
| [`core/di`](src/core/di) | Lazy, typed providers with cycle detection and ordered teardown. |
| [`core/module`](src/core/module) | Apps, modules, the two-phase descriptor. |
| [`core/errs`](src/core/errs) | One error type: a transport-neutral kind, a safe message, an internal cause. |
| [`core/config`](src/core/config) | Environment into a struct, validated at boot, secrets masked in dumps. |
| [`core/logx`](src/core/logx) | `log/slog` with the request's trace, account and actor attached. |
| [`core/ctxkit`](src/core/ctxkit) | Request identity on a context, and `Detach` for background work. |
| [`core/lifecycle`](src/core/lifecycle) | Init, ready, reload, shutdown — reverse order, real drain deadline. |
| [`web/router`](src/web/router) | Routes as data, plus the registry that makes them assertable. |
| [`web/guard`](src/web/guard) | Authorization, per route, visible in the route table. |
| [`web/interceptor`](src/web/interceptor) | Around-the-handler concerns: timing, headers, audit on success. |
| [`web/binding`](src/web/binding) | Generic bind-and-validate with field-scoped errors. |
| [`web/pipe`](src/web/pipe) | Typed parameter extraction that cannot silently yield a zero value. |
| [`web/response`](src/web/response) | The envelope, and the one place a message is scrubbed. |
| [`web/filter`](src/web/filter) | Exception filters; nothing reaches a client unclassified. |
| [`web/pagination`](src/web/pagination) | Bounded page requests with an allow-listed sort. |
| [`web/middleware`](src/web/middleware) | Request id, recover, timeout, CORS, security headers, rate limit. |
| [`web/health`](src/web/health) | Liveness and readiness as the different questions they are. |
| [`core/telemetry`](src/core/telemetry) | Provider-based OpenTelemetry for HTTP, jobs, queues, events, and functions. |
| [`data/repo`](src/data/repo) | The scoped generic repository, and transactions on a context. |
| [`data/gormx`](src/data/gormx) | Driver errors into typed errors; composable query scopes. |
| [`platform/cache`](src/platform/cache) | Memory or Redis; a cache failure is never a request failure. |
| [`platform/events`](src/platform/events) | Typed in-process events; panics contained, contexts detached. |
| [`platform/lock`](src/platform/lock) | Distributed locks that only release their own. |
| [`platform/scheduler`](src/platform/scheduler) | Cron and intervals, singleton and overlap-safe. |
| [`platform/queue`](src/platform/queue) | Bounded in-process workers with retries and dead letters. |
| [`web/webhook`](src/web/webhook) | Constant-time HMAC webhook signing and verification. |
| [`web/sse`](src/web/sse) | Heartbeats, flushing and disconnect-aware SSE transport. |
| [`reliability/retry`](src/reliability/retry) | Backoff with jitter, retrying only what is safe to repeat. |
| [`reliability/breaker`](src/reliability/breaker) | Fail fast on a dependency that is down. |
| [`reliability/idempotency`](src/reliability/idempotency) | `Idempotency-Key`, so a retried POST does not charge twice. |
| [`reliability/outbox`](src/reliability/outbox) | A side effect as durable as the write that caused it. |
| [`testkit`](src/testkit) | The real pipeline, in memory, with the envelope decoded. |

## Testing

```go
func TestCreateRequiresAnAccount(t *testing.T) {
    h := testkit.Mount(t, "/api/v1", catalog.NewApp(scope))

    h.Post("/api/v1/catalog/products", map[string]any{"name": "ab"}).
        ExpectStatus(t, http.StatusUnauthorized).
        ExpectCode(t, "NO_TENANT")
}
```

These go through the real middleware, the real guards and the real exception
filter — which is where most of this framework's behaviour lives, and where a
mock would quietly diverge.

## Versioning and support

[Semantic Versioning](https://semver.org). Before 1.0 the exported API may
change between minor versions, and every such change is listed in
[CHANGELOG.md](CHANGELOG.md) under **Changed** with what to do about it. After
1.0, no exported identifier changes incompatibly within a major version.

Release archives are signed with [cosign](https://docs.sigstore.dev) using the
release workflow's own OIDC identity, and each ships an SBOM.
[RELEASING.md](RELEASING.md) has the process and the verification command.

| | |
|---|---|
| Questions | [Discussions → Q&A](https://github.com/IncepTools/go/discussions/categories/q-a) |
| Bugs | [Open an issue](https://github.com/IncepTools/go/issues/new?template=bug_report.yml) |
| Security | **Not a public issue** — [SECURITY.md](SECURITY.md) |
| Everything else | [SUPPORT.md](SUPPORT.md) |

## Status

**v1.0.0.** Built on Fiber v3 and GORM, both stable. The public API is now
covered by the compatibility policy in [RELEASING.md](RELEASING.md).
The next roadmap phase is contract-first: OpenAPI 3.1 generation and breaking
change checks, a transport-neutral execution context, and a checked provider
graph. Later phases add durable broker adapters, broader security policies,
transport parity, generators, and optional platform integrations. See the
[full roadmap](ROADMAP.md) for priorities and deliberate non-goals.

## Contributing

[CONTRIBUTING.md](CONTRIBUTING.md). The short version: explain the failure a
line prevents, not the mechanism; `make ci` must pass; a fix comes with a test
that fails without it.

Governance is in [GOVERNANCE.md](GOVERNANCE.md), and the bar for a new
dependency is deliberately high — a framework's dependencies become every
importer's.

## License

[Apache 2.0](LICENSE).
