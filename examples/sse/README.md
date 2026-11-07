# sse

A long-lived stream of server-sent events, resumable after a drop.

```bash
go run ./sse

curl -N localhost:8087/api/v1/stream/events
curl -N 'localhost:8087/api/v1/stream/events?interval=500ms'
curl -N -H 'Last-Event-ID: 40' localhost:8087/api/v1/stream/events  # resumes at 41
curl 'localhost:8087/api/v1/stream/events?interval=1ms'             # 400, typed
```

`-N` matters: without it curl buffers and the stream looks frozen.

## Layout

```
apps/stream/                    the domain
  stream_app.go                 Descriptor, Providers, NewApp
  events/                       the module
    events_module.go            Providers, New
    constants/                  module name, event name, query names
    dto/                        Tick
    service/                    the producer, and the interval policy
    controller/                 SSE framing, retry policy, Last-Event-ID
```

The split is the point: the service emits values through a callback and knows
nothing about SSE. The same producer serves a WebSocket, a test that collects
into a slice, or a batch job that writes to a file. The controller owns the
transport.

## Four things a stream has to get right

**Fail before the first byte.** Once the stream opens the status is already
200, and an error after that can only be delivered as an event the client has
to know to look for. The interval is validated in the controller, before
`sse.New` is called.

**Number the events.** `id:` is what the browser replays in `Last-Event-ID`
after a dropped connection. A stream that does not number its events cannot be
resumed, only restarted.

**Name the events.** Clients register listeners per event name. An unnamed
event only reaches `onmessage`, so adding a second kind later silently changes
what existing clients receive.

**Set `retry:`.** The default is to reconnect immediately, which turns a
restart into a stampede.

Two more, in the code: the producer treats cancellation as a normal exit —
a browser closing a tab is not a failure worth alarming about — and the
interval has a floor, because without one a client asking for 1ms makes the
server generate a thousand events a second at the server's expense.

Note that `main.go` sets no request timeout. The middleware chain's timeout
would cancel a stream that is behaving exactly as intended; a long-lived
response needs its own idle policy, which the SSE heartbeat provides.

See [`docs/sse.md`](../../docs/sse.md) for authentication and multi-replica
behaviour.
