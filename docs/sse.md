# Server-sent events

`web/sse` exposes Fiber's SSE transport through the Inceptools web layer. It
handles event framing, flushing, heartbeat comments, reconnect metadata and
client disconnects while the application owns the stream's data source.

## A stream route

```go
app.API().Get("/events", sse.New(sse.Config{
	Retry:            time.Second,
	HeartbeatInterval: 15 * time.Second,
	Handler: func(_ fiber.Ctx, stream *sse.Stream) error {
		return stream.Event(sse.Event{
			Name: "ready",
			ID:   "1",
			Data: map[string]string{"status": "ok"},
		})
	},
}))
```

The adapter sets `text/event-stream`, disables intermediary buffering and
keeps the connection alive. `Event.Data` strings are written as SSE data;
other values are JSON encoded.

## Streaming a source

Use the stream context to stop subscriptions when the browser disconnects:

```go
Handler: func(_ fiber.Ctx, stream *sse.Stream) error {
	updates := store.Subscribe(stream.Context())
	defer updates.Close()

	for {
		select {
		case <-stream.Context().Done():
			return nil
		case update := <-updates.C:
			if err := stream.Event(sse.Event{
				Name: "update",
				ID:   update.ID,
				Data: update,
			}); err != nil {
				return err
			}
		}
	}
}
```

`Stream.Event` flushes each frame. A write error normally means the client
went away; return it so the subscription is released.

## Reconnects

Set an event `ID` so browsers send `Last-Event-ID` when reconnecting. Read it
with `stream.LastEventID()` and replay from a bounded event log if the product
requires lossless reconnects. The SSE transport does not store or replay
events by itself.

`Config.Retry` sends an initial reconnect delay. `Event.Retry` can override it
for one event.

## Heartbeats

Heartbeats are comment frames, not application events. They keep proxies from
closing idle connections and help detect disconnected clients. The default is
15 seconds; set `HeartbeatInterval` for your proxy and load-balancer timeouts,
or use `DisableHeartbeat` only when the stream is guaranteed to be busy.

## Authentication and scale

Authenticate the route before creating a stream. Do not put long-lived user
credentials in the query string. For multiple replicas, an in-memory pub/sub
source only reaches clients connected to that replica; use Redis, a broker or
another shared source when every client must see every update.

SSE is one-way. Use a normal authenticated POST route for client commands and
reserve the stream for server-to-client updates.

## Testing

Test the producer separately from the transport: feed a fake update channel,
cancel the stream context and assert the subscription closes. For an HTTP
test, assert `Content-Type: text/event-stream`, the retry field and one event
frame, then close the client context.

## Example

```bash
cd examples
go run ./sse
curl -N localhost:8087/api/v1/stream/events
```

## Next

- [Events](events.md) — typed in-process events that can feed a stream.
- [Security](security.md) — authentication and response controls.
- [Lifecycle](lifecycle.md) — shutdown behavior for background subscriptions.
