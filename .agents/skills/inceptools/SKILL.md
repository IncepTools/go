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
