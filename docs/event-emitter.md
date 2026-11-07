# Event emitter example

The event-emitter example demonstrates the existing `platform/events` bus in
isolation. The bus is typed at both ends: publishers send a concrete event
type and subscribers receive that same type without `any` or assertions.

## Run it

```bash
cd examples
go run ./event-emitter
curl -X POST localhost:8088/api/v1/events/notifications/orders \
  -H 'content-type: application/json' \
  -d '{"id":"order-1"}'
```

The controller publishes `notifications.OrderPlaced`; the module registers an
async audit subscriber. The HTTP response is returned without waiting for the
audit log.

## Module shape

The example follows the normal structure:

```text
event-emitter/
├── main.go
└── apps/events/
    ├── events_app.go
    └── notifications/
        └── notifications_module.go
```

The module registers its subscriber in `New`, where dependencies are already
available. The controller only binds the request, publishes the event and
renders the response.

## Async behavior

Async handlers receive a detached context. Request cancellation therefore
does not silently cancel an audit or notification. Handler errors and panics
are contained and logged by the bus; they do not change the already-returned
HTTP response.

Use `events.Sync` only when the publisher must fail if the subscriber fails.
For required business rules, a direct service call is usually clearer than a
synchronous event hidden behind a bus.

## Durability boundary

The event bus is process-local and lossy on restart. It is right for metrics,
cache invalidation and best-effort notifications. If another system is owed
the effect, write an outbox row in the same transaction as the state change.
See [Events](events.md) and [Transactional outbox](outbox.md).

## Testing

Register a synchronous test subscriber and assert the concrete event:

```go
bus := events.New()
var got notifications.OrderPlaced
events.On(bus, "test", events.Sync, func(_ context.Context, e notifications.OrderPlaced) error {
	got = e
	return nil
})

if err := events.Publish(context.Background(), bus, notifications.OrderPlaced{ID: "order-1"}); err != nil {
	t.Fatal(err)
}
```

For async subscribers, call `bus.Wait()` before asserting side effects.

## Next

- [Events](events.md) — the complete bus contract.
- [Transactional outbox](outbox.md) — durable side effects.
