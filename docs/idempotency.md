# Idempotency

`reliability/idempotency` makes a repeated POST safe.

A client whose request times out does not know whether it happened. It
retries. Without this, the retry creates a second order and charges for it —
and the client still does not know, so it retries again.

## Installing it

```go
app.Fiber.Use(idempotency.New(idempotency.Options{
	Store: idempotency.NewGormStore(db),
	TTL:   24 * time.Hour,
}))
```

```bash
curl -X POST /api/v1/shop/orders \
     -H 'Idempotency-Key: 7f1c…' \
     -d '{"items":[...]}'
```

The first request executes and its response is recorded against the key. A
later request with the same key returns that recorded response without
executing anything, with `Idempotent-Replayed: true` so the client can tell.

This is the contract API clients already expect.

## Two details that are easy to get wrong

**The key is scoped to the account, the method and the path.** A global key
space lets one account's key collide with another's and serve them someone
else's response body.

**The request body is fingerprinted and compared.** Reusing a key with a
different body is a client bug; returning the old response would hide it, so
it is refused with a 400 and `IDEMPOTENCY_KEY_REUSED`.

## Options

| | |
|---|---|
| `Store` | Required. See below. |
| `TTL` | How long a record is replayable. Default 24h. |
| `Methods` | Default POST, PATCH, DELETE. |
| `Require` | Make a missing key a 400. Off by default. |
| `Skip` | Exempt a request. |

PUT is absent from the defaults because a correct PUT is already idempotent.
GET is absent because it changes nothing.

`Require` breaks every existing client the day you turn it on. Turn it on for
a new API; for an existing one, leave it off and encourage keys first.

## Behaviour

| Situation | |
|---|---|
| Same key, same body | The recorded response, with `Idempotent-Replayed: true`. |
| Same key, different body | 400 `IDEMPOTENCY_KEY_REUSED`. |
| Same key, first request still running | 409 `IDEMPOTENCY_IN_FLIGHT`. |
| No key, `Require` off | Passes through, unprotected. |
| Handler returned 5xx | **Not recorded.** The key is released and can be retried. |
| Store is down | **Fails open**, logged. |

### Why 409 rather than waiting

The client is retrying because it thinks the first call is lost. Blocking
until the original finishes just doubles its timeout.

### Why a 5xx is not recorded

Recording it would pin a transient failure to the key for the whole TTL, and
the client could never succeed with that key again.

### Why it fails open

An idempotency store that is down must not take writes down with it. The cost
is that a retry in that window may duplicate — which is the behaviour without
this middleware at all. Log it and alert on it.

## Stores

```go
idempotency.NewGormStore(db)      // production
idempotency.NewMemoryStore()      // tests, single-instance development
```

Use the **database**, not a cache. The record must survive the restart the
client is retrying through; a cache that was evicted is a duplicate charge.
Redis is only the right choice when it is configured to persist.

The reservation is a single `INSERT ... ON CONFLICT DO NOTHING`, so two
concurrent first-requests cannot both win: exactly one inserts, the other
reads what it wrote. Select-then-insert would leave the race open in the
window between them — precisely the window a retrying client occupies.

### The table

```sql
CREATE TABLE idempotency_records (
    key          TEXT PRIMARY KEY,
    fingerprint  TEXT NOT NULL,
    status       INTEGER NOT NULL DEFAULT 0,
    body         BYTEA,
    content_type TEXT,
    completed    BOOLEAN NOT NULL DEFAULT FALSE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at   TIMESTAMPTZ NOT NULL
);
CREATE INDEX idempotency_records_expires_at_idx ON idempotency_records (expires_at);
```

### Sweep it

```go
sched.MustAdd(scheduler.Job{
	Name: "sweep-idempotency", Cron: "0 4 * * *", Singleton: true,
	Run: func(ctx context.Context) error {
		_, err := store.Sweep(ctx)
		return err
	},
})
```

Without this the table grows by one row per write request, forever.

## This is not a substitute for a natural key

Idempotency keys protect against a **retry**. They do not protect against a
client that generates a fresh key for what is logically the same request — a
double-clicked button often does exactly that.

Where a business rule says "only one of these may exist", enforce it with a
unique constraint as well:

```sql
CREATE UNIQUE INDEX orders_account_reference_idx ON orders (account_id, client_reference);
```

Belt and braces. The key handles the network; the constraint handles the
user.

## Next

- [Transactional outbox](outbox.md) — the other half of exactly-once.
- [Resilience](resilience.md) — the retries this exists to absorb.
