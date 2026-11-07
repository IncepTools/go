# Lifecycle

`core/lifecycle` gives a module three moments — init, ready, shutdown — and
the signal handling that makes shutdown actually happen.

## The phases

```go
type Initializer interface{ OnInit(ctx context.Context) error }
type Ready       interface{ OnReady(ctx context.Context) error }
type Shutdowner  interface{ OnShutdown(ctx context.Context) error }
```

A module (or anything registered with `app.Lifecycle.Observe`) implements
whichever it needs; implementing none is a no-op, not an error.

| | When | For |
|---|---|---|
| `OnInit` | After wiring, **before** the listener opens | Warm a cache, verify a dependency answers. An error here **fails the boot**. |
| `OnReady` | After the listener is up | Start background loops, reconcilers, schedulers. |
| `OnShutdown` | On the way down, under the drain deadline | Stop loops, flush buffers, close clients. |

## Ordering

Hooks run in registration order going **up** and in **reverse** order coming
down:

```
init:  database → cache → worker
down:  worker   → cache → database
```

So a module can safely assume that anything it was wired after is still alive
while it drains. A worker that writes a final batch on shutdown still has its
database.

`Init` **stops at the first failure**, because the hooks after a failed one
were written assuming it succeeded. Whatever did initialise is recorded, so
the following `Shutdown` unwinds exactly that much and no more.

`Ready` and `Shutdown` run **everything** and join the errors. The process is
already serving by then; refusing to start a second background loop because
the first stuttered helps nobody, and one slow client must not keep the rest
from draining.

## Ready, not Init, for background work

```go
func (m *Module) OnReady(ctx context.Context) error { return m.reconciler.Start(ctx) }
func (m *Module) OnShutdown(ctx context.Context) error { return m.reconciler.Stop(ctx) }
```

A loop that starts in `OnInit` begins before the process is serving and races
the boot it is part of — happily running against half-constructed
dependencies. `OnReady` is after the listener is up.

## The drain context

This is the part that is easy to get wrong, and the bug is silent.

When a `SIGTERM` arrives, the application context is cancelled. If you derive
the shutdown context from it, **every shutdown hook sees a context that is
already done** and skips its work — no flush, no drain, no final write — and
nothing reports a problem.

`lifecycle.Run` derives the drain context with `context.WithoutCancel`, so
hooks get a fresh deadline:

```go
dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), drain)
```

Do the same anywhere you write your own teardown.

## Shutdown sequence

`Application.Run` does this:

1. `Container.Eager()` — construct everything. A bad dependency fails the
   deploy rather than the first request that needs it.
2. Register the 404 handler, **after** every module, so a late-mounted route
   is not shadowed.
3. `Init` hooks.
4. Start listening.
5. `Ready` hooks.
6. Wait for `SIGINT`/`SIGTERM`, or for the listener to fail.
7. Stop the listener (in-flight requests finish).
8. `Shutdown` hooks, reverse order, under the drain deadline.
9. `Container.Close()` — closers in reverse construction order.

```go
inceptools.Options{DrainTimeout: 25 * time.Second}
```

Set it below your orchestrator's grace period, or the process is killed
mid-drain. If Kubernetes gives 30 seconds, 25 is about right.

## Registering something that is not a module

```go
sched := scheduler.New(locker)
app.Lifecycle.Observe("scheduler", sched)
```

`Observe` registers whichever interfaces the value implements. Or register a
hook directly:

```go
app.Lifecycle.Add(lifecycle.Hook{
	Name:     "warm-cache",
	Init:     warmCache,
	Shutdown: flushCache,
})
```

## Closers

A provider whose value implements `di.Closer` or `io.Closer` is closed by the
container, in reverse construction order, after the lifecycle hooks:

```go
func (p *Pool) Close(ctx context.Context) error { return p.drain(ctx) }
```

Use a lifecycle hook for behaviour that must stop; use a closer for a
resource that must be released. The two run in that order for a reason: stop
producing work before you close what consumes it.

## Running without the factory

```go
err := lifecycle.Run(ctx, registry, serve, stop, 25*time.Second)
```

For a worker with no HTTP surface, or an embedded application.

## Testing

```go
app.Shutdown(ctx)   // stop the listener, run the hooks, close the container
```

And assert the ordering directly:

```go
r := &lifecycle.Registry{}
r.Add(lifecycle.Hook{Name: "db", Shutdown: record("db")})
r.Add(lifecycle.Hook{Name: "worker", Shutdown: record("worker")})
_ = r.Init(ctx)
_ = r.Shutdown(ctx)
// order == ["worker", "db"]
```

## Next

- [Task scheduling](task-scheduling.md) — the most common `OnReady` participant.
- [Providers](providers.md) — what `Eager` and `Close` operate on.
