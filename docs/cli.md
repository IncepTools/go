# The `icpt` CLI

```bash
go install github.com/IncepTools/go/cmd/icpt@latest
```

Everything reads [`icpt.json`](project-config.md) from the repository root,
found by walking up from the working directory, so a command run inside
`apps/catalog/products` still sees the project's configuration.

Common flags: `-n` is a dry run everywhere, `-root <dir>` overrides the
project root, `-db <name>` picks a database.

## Project

### `icpt init`

Writes `icpt.json`. Keys it does not recognise are preserved, so another
tool's configuration survives.

```bash
icpt init [-apps apps] [-tests colocated|mirror] [-force]
```

Without `-force` it refuses to overwrite and prints what is already there.

### `icpt new <service>`

Scaffolds a service skeleton: `main.go`, a config struct, a `Makefile` and a
`.env.example`.

```bash
icpt new shop -mod github.com/acme/shop
cd shop && go mod init github.com/acme/shop && go get github.com/IncepTools/go
```

### `icpt generate`

```bash
icpt generate module catalog/products   # a full CRUD module
icpt generate resource catalog/products # the same thing
icpt generate app catalog               # just the domain aggregator
```

Writes constants, DTOs, an entity, a repository, a service, a controller and
the module file — and the domain aggregator too when the domain is new,
because a module with nothing to mount it is dead code that compiles.

**It never overwrites.** A generator that clobbers a file destroys work with
no undo, and the one time it happens costs more than every keystroke it ever
saved.

The output honours your layout: directory names, test placement and which
layers to emit all come from `icpt.json`.

Read the generated code before changing it. It already does the things that
are easy to forget — a scoped repository, an allow-listed sort, a guard on
the create route, pointers in the update DTO, typed errors.

### `icpt lint`

```bash
icpt lint [-strict]
```

```
error service-imports-http   apps/catalog/products/service/product_service.go:9
        a service imports github.com/gofiber/fiber/v3
        why: a service that speaks HTTP cannot be called from a scheduled job, a CLI
        or a queue consumer without faking a request. Return an *errs.Error and let
        the controller decide the status.
```

Every finding says what breaks, not just which rule it broke.

| Rule | Default |
|---|---|
| `service-imports-http` | error |
| `controller-imports-db` | error |
| `dto-imports-db` | error |
| `entity-imports-layer` | warn |
| `cross-module-internals` | warn |
| `module-has-no-module-file` | error |
| `app-has-no-root-file` | warn |

Exit code is non-zero on any error, and on any warning with `-strict`.
Severities are configurable — see [`icpt.json`](project-config.md).

### `icpt routes`

```bash
icpt routes [-file routes.json] [-json] [-unguarded]
```

Reads a dump the service writes:

```go
_ = app.WriteRoutes("routes.json")
```

```
METHOD  PATH                           MODULE            GUARDS             SUMMARY
GET     /api/v1/catalog/products       catalog.products  authenticated      List products
POST    /api/v1/catalog/products       catalog.products  authenticated,tenanted  Create a product
```

Commit the dump and it becomes a diffable record of your public surface: an
accidentally added, removed or unguarded route shows up in code review.

`-unguarded` lists mutating routes with no guard and exits non-zero, which
makes it usable as a CI gate.

### `icpt doctor`

```
ok    go.mod found
      module github.com/acme/shop
ok    icpt.json is valid
      apps=apps tests=colocated databases=[primary reporting]
ok    DSN for primary (DATABASE_URL)
FAIL  DSN for reporting (REPORTING_URL)
      export REPORTING_URL before running icpt migrate
ok    migrations for primary (postgres)
ok    apps/ exists
ok    depends on inceptools
ok    go toolchain on PATH
```

## Migrations

Configured under `databases` in `icpt.json`. Full detail in
[Migrations](migrations.md).

### `icpt create <name>`

```bash
icpt create add products table
# created migrations/000003_add_products_table.up.sql
# created migrations/000003_add_products_table.down.sql
```

Takes the name as arguments, or prompts when given none.

### `icpt migrate`

```bash
icpt migrate                    # every configured database
icpt migrate -db primary        # one of them
icpt migrate -n                 # dry run
icpt migrate -to 000012         # stop after a version
icpt migrate -allow-drift       # proceed over an edited applied migration
icpt migrate "postgres://..."   # an explicit DSN, for a one-off
```

With no `-db` it migrates **every** configured database in turn. Applying is
additive and idempotent, so fanning out is safe — and silently skipping a
database is the worse failure.

### `icpt status`

```
VERSION  NAME                 STATE         APPLIED
000001   init                 applied       2026-01-02T11:04:00Z
000002   add_products_table   DRIFTED       2026-01-09T08:31:12Z
000003   add_price_index      pending

primary: 1 pending, 1 problem(s)
```

`DRIFTED` means an applied migration's file was edited afterwards.
`MISSING FILE` means one was applied and then deleted. Either way the ledger
no longer describes a database this checkout can rebuild.

### `icpt down`

```bash
icpt down -steps 1 -db primary
# Reverse 1 migration(s) on "primary"? This can destroy data. Type yes to continue:
```

Unlike `migrate`, it **never** fans out: with several databases configured,
`-db` is required. A rollback across every database because a flag was
omitted is unrecoverable.

It runs the down script **stored in the ledger**, not the file on disk.

`-y` skips the confirmation, for a script that has already decided. `-n` is a
dry run.

## In CI

```yaml
- run: go vet ./...
- run: icpt lint -strict
- run: go test -race ./...
- run: go run ./cmd/dump-routes && git diff --exit-code routes.json
```

The last line catches a route change that was not intended to be part of the
pull request.

## Next

- [`icpt.json`](project-config.md) · [Migrations](migrations.md)
