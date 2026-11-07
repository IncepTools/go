# Queues

`platform/queue` is a lifecycle-aware, in-process worker queue. It provides
bounded buffering, multiple workers, retries and a dead-letter callback. It
is useful for work that should leave the request quickly but does not require
a broker to survive process loss.

## Constructing a queue

```go
jobs, err := queue.New(queue.Options{
	Name:        "emails",
	Workers:     4,
	Buffer:      256,
	MaxAttempts: 3,
	RetryDelay:  time.Second,
	OnDeadLetter: func(ctx context.Context, m queue.Message, err error) {
		logx.Err(ctx, "email job dead-lettered", err, "messageId", m.ID)
	},
}, func(ctx context.Context, m queue.Message) error {
	return mailer.Send(ctx, m.Body)
})
if err != nil {
	return err
}
app.Lifecycle.Observe("emails", jobs)
```

`Name` and the handler are required. Workers default to one, the buffer to
128, maximum attempts to three and retry delay to one second.

## Enqueueing

```go
id, err := jobs.Enqueue(ctx, payload)
return response.Accepted(c, "queued", fiber.Map{"id": id})
```

`Enqueue` copies the payload before placing it in the buffer, so callers can
reuse their request buffer. It waits for buffer capacity or the caller's
context, which provides backpressure instead of silently dropping work.

Return the message id to the client. The id is useful for logs and for a
future status endpoint, but it is not a durable receipt.

## Retry and dead letters

The handler receives the same message id and an incrementing `Attempts` value.
Any returned error causes a retry until `MaxAttempts`; the final failure calls
`OnDeadLetter` once. Successful handlers are removed immediately.

```go
func handle(ctx context.Context, m queue.Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return downstream.Call(ctx, m.Body)
}
```

Make handlers idempotent. A process can fail after the downstream call but
before the handler returns, so a retry may repeat that call.

## Lifecycle and shutdown

Register the queue with the application lifecycle. `OnReady` starts workers
after the listener is ready. `OnShutdown` closes producers and drains the
buffer before returning:

```go
app.Lifecycle.Observe("emails", jobs)
```

The queue's workers use a detached application context. A handler still must
honour cancellation when the shutdown deadline expires. A handler that blocks
forever prevents a clean drain and is a handler bug, not a queue guarantee.

## What it is not

The queue is in memory. A restart loses buffered and in-flight messages. It
does not provide ordering across workers, visibility into historical jobs,
operator-triggered retries or cross-process coordination.

Use [Transactional outbox](outbox.md) when a database write and the queued
side effect must be atomic, then relay to Redis, RabbitMQ, SQS or another
durable broker. Use the scheduler for recurring work, not a queue with a
timer hidden inside a handler.

## Testing

Use a one-worker queue, a short retry delay and a channel or atomic counter:

```go
q, _ := queue.New(queue.Options{
	Name: "test", Workers: 1, MaxAttempts: 2,
	RetryDelay: time.Millisecond,
}, func(context.Context, queue.Message) error {
	return errors.New("try again")
})
```

Assert the attempt count and dead-letter callback. Call `Stop` with a timeout
in tests so a broken handler cannot hang the test process.

## Example

```bash
cd examples
go run ./queues
curl -X POST localhost:8085/api/v1/queues/jobs -d 'build docs'
```

## Next

- [Transactional outbox](outbox.md) — durable hand-off to a broker.
- [Task scheduling](task-scheduling.md) — recurring singleton work.
- [Resilience](resilience.md) — retries and circuit breakers for dependencies.
