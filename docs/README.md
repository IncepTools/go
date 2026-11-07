# Documentation

The [README](../README.md) is the tour. These go deeper, and each one explains
the reasoning as much as the API — a convention whose reason nobody remembers
is a convention that gets dropped the first time it is inconvenient.

## Overview

| | |
|---|---|
| [Introduction](introduction.md) | What this is, what it decides for you, and when not to use it. |
| [First steps](first-steps.md) | From `go mod init` to a running service with one resource. |
| [Modules](modules.md) | Apps, modules, the two-phase descriptor, what a module declares. |
| [Controllers](controllers.md) | Routes as values, and the route registry you can assert over. |
| [Providers](providers.md) | The DI container: lazy construction, cycles, ordered teardown. |
| [Middleware](middleware.md) | The standard chain, and why its order is not a style choice. |
| [Guards](guards.md) | Authorization per route, and why it is not an `if` in the handler. |
| [Interceptors](interceptors.md) | Around-the-handler concerns, composed outside in. |
| [Pipes](pipes.md) | Typed parameter extraction with errors that name the field. |
| [Exception filters](exception-filters.md) | Typed errors, the response funnel, and the leak check. |

## Fundamentals

| | |
|---|---|
| [Configuration](configuration.md) | Environment into a struct, validated at boot. |
| [Validation](validation.md) | Binding, rules, normalisation, defaults, cross-field checks. |
| [Logging](logging.md) | Structured logs with the request's identity attached. |
| [Lifecycle](lifecycle.md) | Init, ready, shutdown — and draining properly. |
| [Testing](testing.md) | Module tests through the real pipeline, and structural tests. |
| [Architecture](architecture.md) | The three levels, the layering rules, and the request pipeline. |

## Data

| | |
|---|---|
| [Database](database.md) | The scoped repository, query scopes, transactions. |
| [Pagination](pagination.md) | Bounded pages and allow-listed sorting. |
| [Migrations](migrations.md) | Versioned SQL across five dialects, and the failure modes. |

## Techniques

| | |
|---|---|
| [Caching](caching.md) | Memory and Redis, and why a cache failure is never a request failure. |
| [Events](events.md) | Typed in-process events, detached contexts, contained panics. |
| [Task scheduling](task-scheduling.md) | Cron and intervals, singleton across replicas, overlap-safe. |
| [Distributed locks](locks.md) | Locks that only release their own, and what they cannot promise. |
| [Idempotency](idempotency.md) | `Idempotency-Key`, so a retried POST does not act twice. |
| [Transactional outbox](outbox.md) | A side effect as durable as the write that caused it. |
| [Resilience](resilience.md) | Retry with jitter, and a circuit breaker. |
| [Health checks](health-checks.md) | Liveness and readiness as the different questions they are. |
| [Security](security.md) | Scoping, authorization, headers, CORS, rate limiting, leak prevention. |

## Tooling

| | |
|---|---|
| [CLI](cli.md) | `icpt`: scaffold, lint, routes, doctor, migrate. |
| [`icpt.json`](project-config.md) | Project configuration: layout, rules, generator, databases. |

---

Every package also carries a doc comment explaining what it is for and which
failure it exists to prevent. `go doc github.com/IncepTools/go/src/data/repo` is
often faster than this directory.
