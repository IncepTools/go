# SSE example

```bash
go run ./sse
curl -N localhost:8087/api/v1/stream/events
```

The stream emits one JSON `tick` event per second and sends heartbeat comments
to keep idle connections alive. See [`docs/sse.md`](../../docs/sse.md) for
reconnects, `Last-Event-ID`, authentication and multi-replica behavior.
