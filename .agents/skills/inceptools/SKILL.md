---
name: inceptools
description: Build and maintain Go services with the inceptools framework and icpt CLI. Use for creating modules, routes, services, repositories, migrations, tests, examples, and framework-consistent changes in an inceptools repository.
---

# inceptools development skill

Use this skill when working in a repository that imports `github.com/IncepTools/go`
or contains `icpt.json`. The goal is to keep the framework's guarantees visible
at boot, in tests, or through `icpt lint`.

## Start here

Read these files before making a non-trivial change:

1. `AGENTS.md` for repository rules and required checks.
2. `README.md` for the public architecture and CLI overview.
3. The relevant guide under `docs/`.
4. The closest runnable example under `examples/`.

Confirm the repository shape before editing:

```bash
go env GOMOD
test -f icpt.json && icpt doctor || true
git status --short
```

The root module is the framework and CLI. `examples/` is a separate Go module;
run commands in the correct module.

## Architecture contract

Keep the dependency direction below:

```text
controller -> service -> repository -> data/database
                         |
                         -> domain errors, events, reliability policies
```

- `src/` contains public framework packages.
- `internal/` contains CLI-only implementation, migrations, linting, and templates.
- `cmd/icpt/` contains the CLI entry point and command wiring.
- `inceptools.go` assembles application lifecycle, middleware, routes, health,
  dependency injection, and graceful shutdown.
- `tests/` mirrors package paths and uses external test packages.
- `examples/` is a separate module and must remain runnable.

For application code, use the conventional layout:

```text
apps/<app>/<resource>/
├── <resource>_module.go
├── constants/
├── controller/
├── dto/
├── entity/
├── repository/
└── service/
```

One module owns one resource. Controllers handle transport concerns, services
hold business rules, repositories handle persistence, and DTOs/entities do not
reach across layers.

## Generate before hand-writing

For a new application project:

```bash
icpt init
icpt doctor
icpt generate app catalog
icpt generate module catalog/products
```

For an existing project, inspect `icpt.json` first and use the generator. The
generator does not overwrite files, so resolve collisions deliberately rather
than deleting user code.

Useful commands:

```bash
icpt lint [-strict]
icpt routes [-json|-unguarded]
icpt doctor
icpt version
```

Treat lint findings as architecture failures, not formatting suggestions. In
particular, services must not import Fiber/HTTP, controllers must not execute
SQL, DTOs must not import database packages, and mutating routes need guards or
an explicit public marker.

## HTTP and errors

- Mount modules through their descriptors; do not bypass application mounting.
- Return transport-neutral `*errs.Error` values from services.
- Keep HTTP status decisions in controllers/response handling.
- Attach guards to routes explicitly so route tables can be inspected.
- Use the framework middleware and lifecycle rather than starting competing
  servers or shutdown goroutines.
- Use `ctxkit` for account/tenant identity and detached background contexts.
- Use `core/telemetry` for OpenTelemetry providers, propagation, error
  recording, and spans. `Options.Telemetry` owns automatic HTTP instrumentation
  and provider shutdown; `telemetry.Run`/`Start` cover arbitrary functions.
  Prefer standard OTLP/HTTP environment variables or `telemetry.OTLPConfig` for
  SigNoz, collectors, APMs, and other compatible backends; inject custom SDK
  providers only when a service needs a specialized pipeline.
- Framework execution boundaries already create spans for scheduler jobs,
  queue attempts, and event handlers. Preserve those boundaries when adding
  workers; do not create duplicate spans in wrappers without a meaningful
  operation name.
- Use `/livez` for liveness and `/readyz` for dependency readiness. Keep
  `/healthz` only as the compatibility alias. Register `Application.OnReload`
  for SIGHUP and call `Application.Reload` from serverless control planes.
- `Application` implements `net/http.Handler`, so server and serverless
  adapters share one configured application and one telemetry pipeline.

## Database and migrations

Never add automatic schema mutation or `AutoMigrate` to production startup.
Create reviewed migration pairs:

```bash
icpt create add_products_table
icpt status
icpt migrate -n
icpt migrate
icpt down -steps 1 -y
```

Files use `<version>_<name>.up.sql` and `.down.sql`. Values must be SQL
parameters. Identifiers must be validated and dialect-quoted; never concatenate
request data into SQL. Preserve the migration ledger, advisory lock, drift
detection, stored rollback scripts, and one-transaction-per-migration behavior.

## Feature selection

Use the existing public packages rather than inventing parallel infrastructure:

- scheduling: `src/platform/scheduler`
- queues: `src/platform/queue`
- events: `src/platform/events`
- locks: `src/platform/lock`
- server-sent events: `src/web/sse`
- webhooks: `src/web/webhook`
- retries, breakers, idempotency, and outbox: `src/reliability`
- OpenTelemetry: `src/core/telemetry`

Read and run the matching example before changing a feature:
`examples/scheduling`, `queues`, `event-emitter`, `sse`, `webhooks`, or `worker`.

## Testing workflow

Put tests in the mirrored `tests/` tree using an external package. Name tests
after guarantees and failure modes. Prefer the real application/testkit path
over mocks that bypass framework behavior.

Run the narrowest check first, then the full checks:

```bash
GOCACHE=/tmp/inceptools-gocache go test ./...
(cd examples && GOCACHE=/tmp/inceptools-examples-gocache go test ./...)
make ci
```

For dependency, security, workflow, or release changes also run:

```bash
make lint
make vuln
make release-dry
```

Run `gofmt` on changed Go files and `git diff --check` before handoff.

## Documentation and generated code

When changing a public capability, update its guide under `docs/`, the relevant
example README, and `CHANGELOG.md` under `Unreleased`. Changes to
`internal/scaffold/templates.go` affect every generated application; update
template tests and generated-code checks with it.

## Safe agent behavior

- Inspect `git status` before editing and preserve unrelated user changes.
- Do not rewrite or delete migrations, releases, tags, or user files without
  explicit authorization.
- Do not commit secrets, database URLs, tokens, generated distributions, or
  temporary databases.
- Prefer additive, reversible changes and report commands that were run.
- Use Conventional Commit subjects: `feat:`, `fix:`, `docs:`, `test:`,
  `refactor:`, or `chore:`.

## AI task playbooks

Use the playbook that matches the request. Each playbook is intentionally
ordered so an agent can execute it without guessing the framework's conventions.

### Add a new CRUD resource

1. Read `docs/first-steps.md`, `docs/modules.md`, `docs/controllers.md`, and
   `docs/database.md`.
2. Inspect `icpt.json`, then run `icpt generate module <app>/<resource>`.
3. Keep the generated module, controller, DTOs, entity, repository, and service
   as separate layers.
4. Add guards to every mutating route and verify with `icpt routes -unguarded`.
5. Add service/repository/module tests in the mirrored `tests/` path.
6. Create reviewed migration pairs if persistence changes.
7. Run `icpt lint -strict`, root tests, examples tests, and `make ci`.

### Add an endpoint to an existing resource

1. Find the module descriptor and existing controller route values.
2. Define or extend a DTO; use pipes/binding for input validation.
3. Put business rules in the service, not the handler.
4. Use the repository for scoped data access.
5. Return a safe response DTO and framework error; never return `err.Error()`.
6. Attach guard/interceptor policy to the route itself.
7. Add a test through the application/testkit pipeline and inspect the route dump.

### Add a scheduled job

1. Read `docs/task-scheduling.md` and `docs/locks.md`.
2. Define a job with a validated cron expression, timeout, lock key, and TTL.
3. Make the handler honor `ctx.Done()` and safe to retry.
4. Use a distributed lock when replicas must be singleton; set TTL above the
   maximum expected runtime.
5. Add a test for cancellation, overlap/skipping, and lock behavior.
6. Use `examples/scheduling` or `examples/worker` as the usage reference.

Every scheduler execution is automatically traced as `scheduler.job`; add
child spans only for meaningful operations inside the job.

### Add observability or runtime reload

1. Read `docs/telemetry.md`, `docs/health-checks.md`, and
   `docs/lifecycle.md`.
2. Configure one `telemetry.Telemetry` instance with the service resource and
   the desired OpenTelemetry SDK exporter or custom providers.
3. Pass it as `inceptools.Options.Telemetry`; the application lifecycle flushes
   providers after workers and modules drain.
4. Use `telemetry.Run`/`Start` for custom functions and propagate context to
   outbound HTTP or messaging clients with `telemetry.Inject`.
5. Use `app.OnReload` for mutable configuration. Test `app.Reload` directly;
   do not make correctness depend on delivering SIGHUP in a serverless test.
6. Test liveness, readiness, reload failure behavior, span error recording,
   and shutdown flush/drain behavior.

### Add queue or event processing

1. Decide whether the work must survive a process crash. If yes, use an outbox;
   in-process events alone are not durable.
2. For queues, define retry count, backoff, dead-letter behavior, and shutdown
   drain behavior before writing the handler.
3. Make the handler idempotent because delivery may be repeated.
4. Keep request contexts out of work that continues after the response.
5. Test retry, cancellation, drain, and duplicate delivery behavior.
6. Read `docs/queues.md`, `docs/events.md`, `docs/outbox.md`, and the matching
   example README.

### Add a webhook consumer

1. Read `docs/webhooks.md` and `docs/security.md`.
2. Verify signature, timestamp, and replay identity at the boundary.
3. Parse and validate into a dedicated DTO.
4. Record durable work/idempotency before acknowledging delivery.
5. Return the provider-required status without exposing internal errors.
6. Test invalid signatures, stale requests, replay, malformed bodies, and retry.

### Add an SSE stream

1. Read `docs/sse.md` and decide authentication and multi-replica delivery first.
2. Register a stream route with explicit guard policy.
3. Send heartbeats and close on request cancellation.
4. Bound subscriber buffers so a slow client cannot exhaust memory.
5. Test authentication, disconnect cleanup, reconnect behavior, and heartbeat.

### Change the CLI or generator

1. Read `docs/cli.md` and inspect command dispatch in `cmd/icpt/main.go`.
2. Keep command parsing, project configuration, and implementation in their
   current packages rather than adding a second CLI path.
3. Update usage text and errors for invalid arguments.
4. If output changes, update the relevant docs and command tests.
5. If generated output changes, update `internal/scaffold/templates.go`, run
   generated compilation, and run `icpt lint -strict` on a temporary project.

## Complete package map

Use this map before introducing a new helper or dependency:

| Concern | Package | Typical questions |
|---|---|---|
| Configuration | `src/core/config` | Is config loaded and validated once at boot? |
| Request identity | `src/core/ctxkit` | Which account/tenant owns this operation? |
| Dependency graph | `src/core/di` | Should this be supplied, provided, named, or scoped? |
| Domain error | `src/core/errs` | What safe kind/code/message reaches the caller? |
| Lifecycle | `src/core/lifecycle` | Does startup/ready/reload/shutdown order matter? |
| Structured logs | `src/core/logx` | What can be correlated without leaking secrets? |
| OpenTelemetry | `src/core/telemetry` | Which boundary, provider, exporter, or propagation is involved? |
| Module metadata | `src/core/module` | What does this resource provide and mount? |
| Routes | `src/web/router` | Can the route be inspected and tested as data? |
| Binding | `src/web/binding` | How are body, path, and query values decoded? |
| Validation | `src/web/pipe` | Is invalid input rejected before business logic? |
| Authorization | `src/web/guard` | Is policy attached to this route explicitly? |
| Cross-cutting HTTP | `src/web/interceptor` | Should this wrap a handler response? |
| Request-wide HTTP | `src/web/middleware` | Should this run for every request? |
| Safe output | `src/web/response`, `src/web/filter` | Can infrastructure details escape? |
| Health | `src/web/health` | Is this liveness, readiness, or detail? |
| Paging | `src/web/pagination` | Are limits, sorting, and stable order bounded? |
| Persistence | `src/data/repo`, `src/data/gormx` | Is account scope applied by construction? |
| Cache | `src/platform/cache` | What is TTL, invalidation, and miss behavior? |
| Events | `src/platform/events` | Is this in-process notification or durable work? |
| Queue | `src/platform/queue` | How are retry, dead letters, and drain handled? |
| Lock | `src/platform/lock` | Must only one replica execute this work? |
| Scheduler | `src/platform/scheduler` | Is cron, timeout, lock, and jitter valid? |
| Retry | `src/reliability/retry` | Is the operation safe and transient to retry? |
| Breaker | `src/reliability/breaker` | Which dependency failure should be isolated? |
| Idempotency | `src/reliability/idempotency` | What repeats safely and what is the request key? |
| Outbox | `src/reliability/outbox` | Must publication survive a process crash? |

## Security and reliability review

Before declaring a change complete, check all applicable items:

- identity and account scope come from trusted context, never request payload;
- mutating routes have explicit guards or an intentional public marker;
- services do not decide HTTP status or serialize infrastructure errors;
- SQL values use parameters; identifiers are validated and dialect-quoted;
- temporary files use `os.CreateTemp` and are cleaned up;
- webhook signatures, replay windows, and idempotency are enforced;
- retries have bounded attempts, delay, timeout, and cancellation;
- circuit breakers wrap one dependency and have observable transitions;
- background work is idempotent and does not retain a request context;
- queues and events have bounded buffers and a shutdown drain policy;
- logs contain correlation data but not tokens, DSNs, credentials, or payload secrets;
- health checks distinguish liveness from dependency readiness;
- migrations are reviewed, reversible where possible, and tested on failure;
- no generated distribution, credential, or local database is committed.

## Verification matrix

Select checks based on the change rather than claiming only a generic test pass:

| Change | Minimum verification |
|---|---|
| `src/` package | focused package tests, mirrored tests, `go test ./...` |
| concurrent feature | `go test -race ./...` and cancellation/drain tests |
| CLI | command tests, `go build ./cmd/icpt`, `icpt doctor` |
| generator/template | temporary generated project build, vet, strict lint |
| migration | dry run, apply, rollback, drift/duplicate tests |
| example | `(cd examples && go test ./...)` and build/run smoke test |
| dependency | tidy, vulnerability scan, license/size rationale |
| workflow/security | YAML validation, full-SHA action review, `make release-dry` |
| public API | docs, example, changelog, compatibility review |

Use task-specific cache paths to avoid contaminating the repository:

```bash
GOCACHE=/tmp/inceptools-gocache go test ./...
(cd examples && GOCACHE=/tmp/inceptools-examples-gocache go test ./...)
```

## Documentation index

Use the narrow guide instead of guessing from names:

```text
first-steps.md       boot a service
architecture.md      package and request flow
project-config.md    icpt.json
modules.md           module descriptors and providers
providers.md         DI and scopes
controllers.md       route values and handlers
guards.md             authorization
interceptors.md      request/response wrappers
middleware.md        HTTP chain
pipes.md, validation.md input validation
exception-filters.md errors and envelopes
health-checks.md     liveness/readiness
serverless.md        net/http/Lambda adapters and reload without signals
telemetry.md         OpenTelemetry providers, spans, metrics, and propagation
                     across HTTP, workers, queues, events, and functions
database.md          scoped repository and transactions
migrations.md        SQL migration lifecycle
caching.md           cache policy
locks.md              distributed locks
task-scheduling.md   cron jobs
queues.md            background queues
events.md            in-process events
outbox.md            durable publication
resilience.md        retry and breaker
idempotency.md       repeatable mutations
sse.md, webhooks.md  streaming and external delivery
testing.md           test strategy
security.md          launch checklist
cli.md               command reference
```
