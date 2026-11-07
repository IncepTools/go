# Introduction

inceptools is an application framework for Go HTTP services. It takes the
decisions that every service in an organisation ends up making separately —
where logic lives, what an error looks like on the wire, who filters a query by
account, how a scheduled job behaves on three replicas — and makes them once.

## What it decides for you

**The shape of a codebase.** Three levels: a domain, a resource inside it, and
the layers of that resource. A controller reads requests. A service holds
rules. A repository talks to the database. These are not suggestions; `icpt
lint` fails a build when a service imports HTTP or a controller imports SQL.

**The shape of a response.** One envelope for every reply, success or failure,
with a trace id a user can quote in a support ticket. One funnel every response
passes through, so the check for leaked infrastructure detail happens in one
place rather than four hundred.

**The shape of an error.** One type carrying a transport-neutral kind, a
machine-readable code, a caller-safe message and an internal cause that is
logged and never serialised. A service that returns `errs.Conflict(...)` has
already decided it is a 409, without importing `net/http`.

**When things fail.** Configuration is a struct loaded once at boot.
Dependencies are constructed eagerly before the listener opens. A missing
environment variable, a malformed cron expression, a module with no name, two
modules on one path — all of these stop the process at start-up rather than
surprising somebody at 2am.

## What it does not do

It does not hide Fiber. `app.Fiber` is right there, and everything this
framework adds is additive. When a route needs something the framework has no
opinion about, you reach past it and nothing breaks.

It does not autowire by struct tag or reflection. A constructor asks for what
it needs, in code, and a missing dependency is a boot-time error naming both
sides. Magic that fails at runtime is worse than a line of wiring.

It does not generate your schema. Migrations are versioned SQL you write and
review; nothing calls `AutoMigrate` on a production database behind your back.

## The thread running through it

Most of what is here exists to close a specific failure, and the comments say
which. A few examples, because they are the argument for the whole design:

- A repository that is *asked* to filter by account will one day be called by
  a query that forgets, and that query returns another customer's rows and
  raises no error. So the repository filters itself and refuses a query with
  no account on the context.
- A handler that returns `err.Error()` will one day return a driver message
  containing a host and a constraint name. So one funnel scrubs outgoing
  messages, and an unclassified error becomes a generic 500 with its original
  in the log.
- A guard added with `r.Use(...)` protects every route *after* it, so a public
  route added below a protected one is silently exposed. So guards attach per
  route, and the route table can be asserted over in a test.
- A background goroutine handed the request context dies when the client
  disconnects, and the side effect silently never happens. So async work gets
  a detached context that keeps the trace id and drops the cancellation.

None of these are exotic. They are the ordinary mistakes of building services,
and a framework earns its place by making them hard rather than by adding
features.

## When not to use it

- **A single-purpose program.** A CLI, a one-off importer, a worker with no
  HTTP surface. The structure costs more than it returns.
- **A service that is genuinely one handler.** Reach for `net/http`.
- **A team that wants a different layering.** The conventions are
  configurable, not negotiable in spirit: if you disagree with services being
  free of HTTP, this framework will annoy you on every file.

## Next

[First steps](first-steps.md) builds a working service. [Architecture](architecture.md)
explains the layering and the request pipeline.
