# Examples

Four services, each runnable with one command and nothing installed.

They are a **separate Go module** on purpose: they need dependencies the
framework does not — a GORM dialector, a database driver — and anything in
the root `go.mod` becomes a line in the `go.sum` of every project that
imports this library.

```bash
cd examples
go run ./minimal    # :8080
go run ./todo       # :8080
go run ./blog       # :8081
go run ./worker     # :8082
```

Or from the repository root: `make examples` builds and vets all four.

| | Port | Shows |
|---|---|---|
| [minimal](minimal) | 8080 | The floor: one domain, one module, one route, ~60 lines. |
| [todo](todo) | 8080 | In-memory CRUD, events, a scheduled purge, the three DTO hooks. |
| [blog](blog) | 8081 | A real database: migrations, a scoped repository, a transaction spanning a state change and an outbox message, and the relay that delivers it. |
| [worker](worker) | 8082 | A service with **no API**: modules that declare schedules instead of routes, and a drain you can watch. |

## Where to start

**New to the framework** — read [`minimal`](minimal/main.go) top to bottom.
It is one file and it uses nothing optional.

**Building a CRUD API** — [`todo`](todo) is the shape, and
[`blog`](blog) is the same shape with a database behind it.

**Building a worker** — [`worker`](worker). A module does not have to serve
routes to be a module.

## What each one demonstrates

### minimal

The smallest useful service. A domain built inline, one public route, a
typed error that picks its own status. Everything else in the framework is
optional and none of it appears here.

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

**Real authentication.** All three HTTP examples read the account from a
request header, and each says in a comment that a real service must never
do that. Copy the *shape* — identity establishes who the caller is, a guard
decides what they may do — not the mechanism.

**Migrations as a deploy step.** `blog` applies its own SQL at start-up so
that `go run` is the only command you need. A real service runs
`icpt migrate`.
