# Examples

Ten services, each runnable with one command and nothing installed.

They are a **separate Go module** on purpose: they need dependencies the
framework does not — a GORM dialector, a database driver — and anything in
the root `go.mod` becomes a line in the `go.sum` of every project that
imports this library.

```bash
cd examples
go run ./minimal       # :8080
go run ./todo          # :8080
go run ./blog          # :8081
go run ./worker        # :8082
go run ./webhooks      # :8084
go run ./queues        # :8085
go run ./scheduling    # :8086
go run ./sse           # :8087
go run ./event-emitter # :8088
go run ./serverless    # :8089
```

Or from the repository root: `make examples` builds and vets all ten.
Run one service at a time when examples share a port, or choose a different
`Addr` in its `main.go`.

HTTP examples expose `/livez` for liveness and `/readyz` for readiness;
`/healthz` remains available as a compatibility alias. The worker example has
the same probe surface even though it declares no business routes.

For production observability, create one `core/telemetry.Telemetry` during
startup and pass it as `inceptools.Options.Telemetry`. The same provider then
traces HTTP requests, scheduled jobs, queues, events, and custom functions;
see [`docs/telemetry.md`](../docs/telemetry.md).

| | Port | Shows |
|---|---|---|
| [minimal](minimal) | 8080 | The floor: one domain, one module, one route — and every layer around them. |
| [todo](todo) | 8080 | In-memory CRUD, events, a scheduled purge, the three DTO hooks. |
| [blog](blog) | 8081 | A real database: migrations, a scoped repository, a transaction spanning a state change and an outbox message, and the relay that delivers it. |
| [worker](worker) | 8082 | A service with **no API**: modules that declare schedules instead of routes, and a drain you can watch. |
| [webhooks](webhooks) | 8084 | Signed webhook verification and event dispatch. |
| [queues](queues) | 8085 | Retrying in-process worker queue. |
| [scheduling](scheduling) | 8086 | Singleton scheduled jobs and lifecycle draining. |
| [sse](sse) | 8087 | Server-sent event streaming. |
| [event-emitter](event-emitter) | 8088 | Typed asynchronous event subscribers. |
| [serverless](serverless) | 8089 | The same application hosted as an `http.Handler`: `Prepare` instead of `Run`, `Reload` instead of `SIGHUP`. |

## One layout, ten times

Every example has the same shape, because it is the shape
[`icpt generate`](../docs/cli.md) produces and the one worth copying:

```
main.go                         builds the application and mounts the domain
icpt.json                        where the generator puts things
apps/<domain>/
  <domain>_app.go               Descriptor, Providers, NewApp
  <module>/
    <module>_module.go          Providers, New, and the module's lifecycle
    constants/                  names and messages, spelled once
    dto/                        request and response shapes
    service/                    the business rules — imports nothing from web/
    controller/                 the HTTP surface — holds no rules
```

**A module owns everything that is its own.** Its queue, its schedule, its
subscriptions, its reloadable configuration, and its start and drain. A
mounted module is a lifecycle participant, so implementing `OnReady`,
`OnShutdown` or `OnReload` is the whole registration — there is no second step
in `main` to forget.

**A domain owns what its modules share.** The event bus, the scheduler engine:
one per domain, because two would mean a publisher and its subscriber never
meeting, or two sets of singleton locks.

**`main.go` owns the process.** The logger, the address, the middleware chain,
the drain timeout — and then it mounts a domain. In most of these examples
that is all it does.

## Where to start

**New to the framework** — read [`minimal`](minimal) top to bottom. It is the
whole layout with the least possible content in it, and it uses nothing
optional.

**Building a CRUD API** — [`todo`](todo) is the shape, and
[`blog`](blog) is the same shape with a database behind it.

**Building a worker** — [`worker`](worker). A module does not have to serve
routes to be a module.

**Deploying to Lambda** — [`serverless`](serverless). The application is the
same; only the host around it changes.

## What each one demonstrates

### minimal

The smallest **complete** service — every layer above, with about 25 lines of
behaviour inside it. One public route and a typed error that picks its own
status. Smallest complete, not smallest possible: the layout is the part worth
copying, and a one-file version teaches a shape nobody should grow.

### todo

```bash
go run ./todo

ACC=11111111-1111-1111-1111-111111111111
USR=22222222-2222-2222-2222-222222222222

curl -X POST localhost:8080/api/v1/todo/tasks \
     -H 'Content-Type: application/json' \
     -H "X-Account-Id: $ACC" -H "X-User-Id: $USR" \
     -d '{"title":"Write the docs","priority":"high","tags":["  Writing  "]}'
```

Watch the response: the tag came back `"writing"` (Normalize), the priority
defaulted to `normal` when omitted (SetDefaults), and a `due_at` in the past
is refused by a cross-field rule (Validate). Completing a task publishes an
event with an async subscriber; a singleton job purges old ones nightly.

Try `?sort=password` to see the sort allow-list refuse it, and omit the
headers to see the guard refuse the write.

### blog

```bash
go run ./blog
```

The one with a database. SQLite, so there is nothing to install, but the
code is the same code you would run against PostgreSQL:

- **Versioned SQL** in [`migrations/`](blog/migrations), including the
  unique index that makes the service's duplicate check actually true.
- **A scoped repository** — every query filtered by account, and a query
  with no account refused. Ask for the list as a different account and it
  is empty.
- **A transaction** in `Publish` that marks the post and writes the outbox
  message together. Either both land or neither does.
- **The relay** delivering that message a second later, logged.
- **A custom validation rule** (`slug`) registered with its own message.
- **A row lock** (`LockByID`), so two concurrent publishes cannot both emit
  an event.

### worker

```bash
go run ./worker
curl localhost:8082/health
```

No API. The module declares two schedules and no controllers. Press Ctrl-C
and watch the drain: the scheduler stops taking occurrences, the run in
flight finishes under the deadline, and `OnShutdown` reports what it did.

A job that fails is logged and the schedule continues — one of them fails
at random to show that. A job cancelled by shutdown is reported as stopped,
not failed, because a routine deploy should not page anybody.

## What they do not show

**Real authentication.** The HTTP examples read the account from a
request header, and each says in a comment that a real service must never
do that. Copy the *shape* — identity establishes who the caller is, a guard
decides what they may do — not the mechanism.

**Migrations as a deploy step.** `blog` applies its own SQL at start-up so
that `go run` is the only command you need. A real service runs
`icpt migrate`.
