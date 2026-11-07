# Transactional outbox

`reliability/outbox` makes a side effect as durable as the write that caused
it.

## The problem

A handler writes a row and then publishes an event. Between those two lines
the process can die, and you have an order that exists with nothing
downstream knowing. Swap the order and you have an event for an order that
does not exist.

There is no ordering of two systems that fixes this. The write and the
publish must be **one atomic act**.

## The fix

Write the message into a table in the same transaction as the state change.
Either both land or neither does. A relay then reads the table and publishes,
retrying until it succeeds.

```go
err := repo.Transaction(ctx, db, func(ctx context.Context) error {
	if err := orders.Create(ctx, o); err != nil {
		return err
	}
	return outbox.Enqueue(ctx, db, "order.placed", o)
})
```

`Enqueue` **refuses** to run outside a transaction. Outside one it is just a
slower publish with the same hole in the middle, and silently allowing that
would defeat the entire package.

## At-least-once

The relay can publish twice — it may crash between publishing and marking the
row. **Consumers must be idempotent.** Exactly-once does not exist here;
at-least-once plus an idempotent consumer is the design.

In practice that means the consumer keys on the message id, or the operation
is naturally repeatable.

## The relay

```go
relay := outbox.NewRelay(outbox.RelayOptions{
	DB:        db,
	Publisher: outbox.PublisherFunc(publishToBroker),
	Batch:     50,
	Interval:  5 * time.Second,
	MaxAttempts: 10,
	Backoff:   10 * time.Second,
})

app.Lifecycle.Observe("outbox", relay)
```

It is **safe to run on every replica**. Each pass claims rows with
`FOR UPDATE SKIP LOCKED`, so two relays take disjoint batches rather than
fighting or double-publishing. That is why this needs no distributed lock.

## The publisher

```go
func publishToBroker(ctx context.Context, m outbox.Message) error {
	return broker.Publish(ctx, m.Topic, m.Payload)
}
```

An error means "try again later" — the message is rescheduled with
exponential backoff.

Return `outbox.ErrDrop` for a message that will never succeed, such as a
malformed payload from an old deployment, so the relay stops retrying it
forever.

## Parked messages

After `MaxAttempts`, a message is **parked**: left unpublished, with its last
error, for a human.

```go
stuck, err := relay.Parked(ctx, 50)
```

Alert on this being non-empty. It is a side effect that was promised and
never happened — which is exactly the thing the outbox exists to make
visible rather than silent.

Parking rather than deleting is deliberate: a message nobody can explain is
evidence.

## The table

```sql
CREATE TABLE outbox_messages (
    id            UUID PRIMARY KEY,
    topic         TEXT NOT NULL,
    payload       JSONB NOT NULL,
    trace_id      TEXT,
    attempts      INTEGER NOT NULL DEFAULT 0,
    last_error    TEXT,
    available_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at  TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX outbox_pending_idx ON outbox_messages (available_at)
    WHERE published_at IS NULL;
```

The partial index keeps the pending scan cheap however large the history
grows.

`trace_id` carries the originating request's id, so a publish hours later, in
a different process, correlates back to the API call that caused it.

## Housekeeping

Published rows accumulate. Delete them on a schedule, keeping enough history
to answer questions:

```go
sched.MustAdd(scheduler.Job{
	Name: "prune-outbox", Cron: "0 4 * * *", Singleton: true,
	Run: func(ctx context.Context) error {
		return db.WithContext(ctx).
			Where("published_at < ?", time.Now().AddDate(0, 0, -7)).
			Delete(&outbox.Message{}).Error
	},
})
```

## When to use it

**Use it** when another system is owed something: a broker message, a
webhook, an email, a payment capture, a search index update.

**Do not** use it for an in-process side effect with no external consequence
— an audit line, a cache invalidation, a metric. [Events](events.md) are
lighter and the loss is acceptable.

The test: *if this is silently lost, does somebody outside this service end
up with the wrong picture?* If yes, outbox.

## Testing

```go
n, err := relay.Pass(ctx)   // one batch, deterministically
```

`Pass` is exported so a test can drive the relay without waiting on a ticker.

## Next

- [Events](events.md) — the lighter, lossy alternative.
- [Idempotency](idempotency.md) — the consumer's half of the contract.
