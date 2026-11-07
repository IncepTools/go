# event-emitter

One typed event, fanned out to subscribers that cannot fail the publisher.

```bash
go run ./event-emitter

curl -X POST localhost:8088/api/v1/events/notifications/orders \
  -H 'Content-Type: application/json' \
  -d '{"reference":"A-1","email":"buyer@example.com","total_cents":2500}'

# Refused before anything is published, so no email is sent:
curl -X POST localhost:8088/api/v1/events/notifications/orders \
  -H 'Content-Type: application/json' \
  -d '{"reference":"A-2","email":"buyer@example.com","total_cents":2000000}'
```

Watch the log order: stock is reserved before the response, the confirmation
and the audit arrive after it.

## Layout

```
apps/events/                    the domain
  events_app.go                 Descriptor, Providers (the bus), NewApp
  notifications/                the module
    notifications_module.go     Providers, New — and the subscriptions
    constants/                  module name, subscriber names, messages
    dto/                        PlaceOrder, OrderReceipt
    service/                    OrderService (publishes), SubscriberService (consumes)
    controller/                 the HTTP surface
```

The subscriptions live in the module that owns both the event and its
consequences. Registering them in `main` scatters the answer to "what happens
when an order is placed" across two files, and the module stops being movable
on its own.

The bus is one level up, in the domain, because every module publishes and
subscribes on the same one. Two buses would mean a publisher and its
subscriber never meeting — and nothing would report an error, because a bus
with no subscribers is a legitimate state.

## The thing worth knowing about `Publish`

**A subscriber cannot veto an event.** `Publish` dispatches every async
subscriber as it walks the list, so by the time a `Sync` subscriber returns an
error the async ones have already started. A `Sync` subscriber can report a
failure; it cannot prevent the consequences.

So the stock check is a step in `OrderService.Place`, before the publish — not
a subscriber. Get that backwards and the order is rejected with a 409 while
the customer still receives a confirmation email.

The rule that falls out: **a prerequisite is a step, a consequence is a
subscriber.**

## Sync and async

Both subscribers here are `Async`, which is the normal case. A consequence
should not be able to slow the response or fail it.

An async handler gets a **detached** context: it keeps the trace id and the
tenant, so its logs still join the request, but it drops the deadline and the
cancellation — a side effect must not die because the client hung up.
Shutdown still waits for it, because the bus tracks in-flight handlers.

The trade is that an async handler's error reaches no caller. The bus logs it,
and you find it in the logs rather than in the response.

See [`docs/events.md`](../../docs/events.md).
