# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
Before 1.0 the exported API may change between minor versions; every such
change is listed under **Changed** with what to do about it.

## [Unreleased]

## [0.1.0] — 2026-11-07

First release.

### Added

**Core**

- `core/di` — lazy, typed providers. Constructed once under concurrency,
  cycles reported with the full path rather than deadlocking, and `Close`
  releasing instances in reverse construction order.
- `core/module` — apps, modules and the two-phase `Descriptor`: every
  provider in the application is registered before any module is built.
- `core/errs` — one error type with a transport-neutral `Kind`, a machine
  `Code`, a caller-safe `Message` and an internal `Cause` that is logged and
  never serialised.
- `core/config` — environment into a struct via tags, with defaults, required
  fields, durations, slices, maps, nested prefixes and `.env` support. Every
  problem is reported in one pass; secrets are masked in `Describe`.
- `core/logx` — `log/slog` with the request's trace id, tenant and actor
  attached, and `Err` for logging the cause a client never sees.
- `core/ctxkit` — request identity on a context, and `Detach` for work that
  outlives the request.
- `core/lifecycle` — init, ready and shutdown hooks; reverse-order teardown
  of exactly what initialised, and a drain context that is not derived from
  the cancelled one.

**Web**

- `web/router` — routes as values, with guards, interceptors and OpenAPI
  metadata attached, plus a registry that answers `Unguarded()`.
- `web/guard`, `web/interceptor`, `web/pipe`, `web/filter`.
- `web/binding` — `Body[T]`, `Query[T]`, `Params[T]`, `Header[T]` with
  field-scoped errors named after the JSON fields, and the `Normalize` /
  `SetDefaults` / `Validate` hooks.
- `web/response` — one envelope, one funnel, one place to install a sanitiser.
- `web/pagination` — bounded pages with an allow-listed sort key.
- `web/middleware` — request id, recover, timeout, access log, identity,
  security headers, CORS and rate limiting, plus `Chain` in the correct order.
- `web/health` — liveness that checks nothing, readiness that checks
  dependencies concurrently, and `Optional` severity that degrades rather
  than downs.

**Data**

- `data/repo` — a generic repository that applies the tenant filter itself
  and refuses a query with no tenant; `UpdateFields` takes a map and refuses
  an empty condition; transactions propagate on the context.
- `data/gormx` — driver errors into typed errors, and composable scopes
  including `ForUpdate`.

**Platform and reliability**

- `platform/cache` (memory and Redis), `platform/events`, `platform/lock`,
  `platform/scheduler`.
- `reliability/retry`, `reliability/breaker`, `reliability/idempotency`
  (with GORM and in-memory stores), `reliability/outbox`.

**Tooling**

- `testkit` — the real pipeline in memory, with the envelope decoded.
- `icpt` CLI: `init`, `new`, `generate app|module|resource`, `lint`, `routes`,
  `doctor`, and the migration commands `create`, `migrate`, `status`, `down`.
- `icpt.json` project configuration: layout, generator toggles, lint rule
  severities, and migration targets. Both the single-database and the
  multi-database shapes are read, and unknown keys are preserved on write so
  another tool's section survives.
- Migrations for **PostgreSQL, MySQL, SQLite, SQL Server and ClickHouse**,
  with per-database dialect, ledger table and lock settings. Duplicate
  versions refused, one transaction per migration where the engine allows it,
  an advisory lock around the run, drift detection, and rollback from the
  stored script rather than the file.
- Documentation under `docs/`, one page per capability, and a runnable
  example service under `examples/todo`.

[Unreleased]: https://github.com/IncepTools/go/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/IncepTools/go/releases/tag/v0.1.0
