# Events

`platform/events` is a typed, in-process event bus: no `any` at the call
site, no type assertion in the handler.

Use it to decouple a side effect from the thing that caused it. Checkout
publishes `OrderPlaced`, and the audit log, the email sender and the usage
meter each subscribe without the checkout code importing any of them.

## Publishing and subscribing

```go
type OrderPlaced struct {
	OrderID   uuid.UUID
	AccountID uuid.UUID
	Total     int64
}

events.On(bus, "orders.email-receipt", events.Async,
	func(ctx context.Context, e OrderPlaced) error {
		return mailer.SendReceipt(ctx, e.OrderID)
	})

_ = events.Publish(ctx, bus, OrderPlaced{OrderID: id, AccountID: acct, Total: total})
```

Subscribe in the module's `New`, where the dependencies are available.

## Async or Sync

| | |
|---|---|
| `events.Async` | Own goroutine, **detached** context. `Publish` does not wait and never sees the error. |
| `events.Sync` | Inline. `Publish` waits and returns the error. |

**Async is the right default** for a side effect: an audit write must not slow
down, or fail, the request that produced it.

Use `Sync` only when the publisher genuinely must not proceed if the handler
fails — and prefer a direct function call in that case, because a required
dependency hidden behind an event bus is harder to follow than a call.

## What it does that a goroutine does not

**A handler panic is contained.** One bad subscriber cannot take down the
publisher, which in an HTTP service is the request.

**Async handlers get a detached context.** It keeps the trace id and the
account and drops the cancellation. Handing a handler the request context
means the side effect is cancelled when the client disconnects — the
notification silently never sends, and nothing reports it.

**`Publish` waits for the sync handlers** and returns their errors, so an
effect that must happen can be made to block the write that caused it.

## Naming subscribers

```go
events.On(bus, "orders.email-receipt", events.Async, ...)
```

The name is what appears in the log when a handler fails. It is the
difference between "an event handler failed" and a line you can act on. Use
`<module>.<what-it-does>`.

## Draining on shutdown

```go
app.Lifecycle.Observe("events", bus)
```

The bus implements `lifecycle.Shutdowner` and waits for in-flight async
handlers under the drain deadline. Without it, a `SIGTERM` during a burst of
publishes drops the side effects that were still running.

## What it is not

**Durable.** The process dying loses every unhandled event.

When the effect must survive a crash, write it to a table in the same
transaction as the state change and publish from there — see
[Transactional outbox](outbox.md).

```go
// In-process events: fine for an email, an audit line, a cache invalidation.
_ = events.Publish(ctx, bus, OrderPlaced{...})

// Outbox: required for anything another system is owed.
return repo.Transaction(ctx, db, func(ctx context.Context) error {
	if err := orders.Create(ctx, o); err != nil {
		return err
	}
	return outbox.Enqueue(ctx, db, "order.placed", o)
})
```

**A queue.** There is no retry, no ordering guarantee across types, and no
backpressure. An async handler that blocks forever holds a goroutine forever.

**Cross-service.** It is in-process. For another service, publish to a broker
— from the outbox, so the publish and the write cannot disagree.

## Ordering

Handlers for one event type run in subscription order; sync ones inline,
async ones concurrently. **Do not depend on ordering between handlers.** Two
handlers that must run in sequence are one handler.

## Diagnostics

```go
bus.Subscribers()   // map[eventType][]handlerName
```

Log it at boot. A subscriber that silently never registered is otherwise
invisible.

## Testing

```go
bus := events.New()
var got []OrderPlaced
events.On(bus, "test", events.Sync, func(_ context.Context, e OrderPlaced) error {
	got = append(got, e)
	return nil
})

_ = svc.PlaceOrder(ctx, req)
// assert on got
```

Subscribe synchronously in tests so there is nothing to wait for. For a real
async subscriber, `bus.Wait()` blocks until the in-flight handlers finish.

## Next

- [Transactional outbox](outbox.md) — when the effect must survive a crash.
- [Lifecycle](lifecycle.md) — draining the bus on shutdown.
