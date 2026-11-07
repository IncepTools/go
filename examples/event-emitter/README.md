# Event emitter example

```bash
go run ./event-emitter
curl -X POST localhost:8088/api/v1/events/notifications/orders \\
  -H 'content-type: application/json' -d '{"id":"order-1"}'
```

The typed subscriber receives the event asynchronously.

The publisher does not wait for the audit subscriber. The bus detaches the
request cancellation and drains in-flight handlers during shutdown. See
[`docs/event-emitter.md`](../../docs/event-emitter.md) and
[`docs/events.md`](../../docs/events.md).
