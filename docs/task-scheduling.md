# Task scheduling

`platform/scheduler` runs recurring work on a cron expression or a fixed
interval, with the two properties that decide whether scheduled work is safe
to deploy on more than one replica.

```go
sched := scheduler.New(locker)

sched.MustAdd(scheduler.Job{
	Name:      "expire-abandoned-carts",
	Cron:      "0 3 * * *",
	Singleton: true,
	Timeout:   10 * time.Minute,
	Jitter:    30 * time.Second,
	Run:       svc.ExpireAbandoned,
})

app.Lifecycle.Observe("scheduler", sched)
```

Registering it with the lifecycle starts it in `OnReady` — after the listener
is up — and drains it on the way down.

## The two properties

**Singleton**: one replica runs each occurrence, via a distributed lock.
**Overlap**: a slow run does not start a second copy of itself.

Both failure modes are invisible in development, where there is one replica
and every job finishes in a millisecond. In production they are how a nightly
reconciliation runs three times, and how a job that normally takes 20 seconds
piles up 200 copies the night it takes 20 minutes.

## The job

| Field | |
|---|---|
| `Name` | Required, unique. It is the log label and the lock key. |
| `Cron` | A 5-field expression, or `@hourly`, `@daily`, `@every 1h30m`. |
| `Every` | A fixed interval from start-up. Set this **or** `Cron`, not both. |
| `Run` | The work. It **must** honour `ctx`. |
| `Singleton` | One replica per occurrence. Requires a `lock.Locker`. |
| `LockTTL` | Must exceed the worst-case runtime. Defaults to `Timeout`. |
| `Timeout` | Bounds one run. Defaults to 5 minutes; zero is **not** unlimited. |
| `RunOnStart` | Fire once at start-up as well. |
| `Jitter` | Spread the start by up to this much. |

### Everything is validated at `Add`

A malformed cron expression, a job with no name, two jobs with the same name,
`Singleton` with no locker, both `Cron` and `Every` — all are errors at
registration. `MustAdd` panics, which is correct at boot: a scheduled job
that cannot run should stop the deploy, not be discovered the night it was
supposed to fire.

### LockTTL must exceed the runtime

Too short and the lock expires mid-run, letting a second replica start — the
exact thing `Singleton` was for. Set it generously; a lock held slightly too
long costs one skipped occurrence.

### Timeout is not optional

The default is five minutes. An unbounded scheduled job is how a connection
pool is exhausted overnight. If a job legitimately takes an hour, say so.

### Honour the context

```go
func (s *Service) ExpireAbandoned(ctx context.Context) error {
	for _, batch := range batches {
		if err := ctx.Err(); err != nil {
			return err              // shutdown, or the timeout
		}
		if err := s.expire(ctx, batch); err != nil {
			return err
		}
	}
	return nil
}
```

A job that ignores its deadline cannot be stopped. Shutdown waits for it
until the drain timeout and then abandons it mid-write.

### Jitter

With `Singleton`, it avoids a thundering herd on the lock. Without it, it
avoids every replica hitting the database on the same second. Thirty seconds
on a job that runs hourly costs nothing.

## Behaviour

**Overlap is skipped, not queued.** A run still going when the next is due
logs a warning and is dropped. Queueing a slow job behind itself converts a
slowdown into an unbounded backlog.

**A panic does not stop the schedule.** It is logged with its stack and the
next occurrence fires.

**An error does not stop the schedule** either. It is logged at error level
with the job name and the duration.

**Each run gets its own trace id**, so everything one occurrence causes is
correlated — including work handed to a goroutine.

## Cron expressions

Standard five fields — minute, hour, day of month, month, day of week — plus
`@hourly`, `@daily`, `@weekly`, `@monthly`, `@yearly` and `@every <duration>`.

```
"0 3 * * *"      03:00 daily
"*/15 * * * *"   every quarter hour
"0 9 * * 1"      09:00 on Mondays
"@every 30s"
```

Times are the **process's local time**. Pin `TZ` in your deployment, or a
nightly job moves by an hour twice a year.

## Choosing the lock

```go
scheduler.New(lock.NewRedis(client, "shop"))   // several replicas
scheduler.New(lock.NewMemory())                // one replica, development
scheduler.New(lock.Noop{})                     // a dedicated cron deployment
```

`lock.Noop` grants every lock — correct when exactly one replica runs the
scheduler by deployment rather than by coordination. Say that in a comment
where you use it.

See [Distributed locks](locks.md) for what the lock can and cannot promise.

## Testing

Test the work, not the schedule:

```go
func TestExpireAbandonedRemovesOldCarts(t *testing.T) {
	err := svc.ExpireAbandoned(testkit.Context(acct, user))
	...
}
```

The job's `Run` is an ordinary method taking a context, which is the whole
reason for keeping services free of HTTP. For the scheduler itself, `Every`
plus `RunOnStart` makes a deterministic test.

## When to reach for something else

This is an in-process scheduler. It has no persistence, no history, and no
way to trigger a run by hand.

If you need a job that survives a restart mid-run, a retry policy per job, or
an operator pressing "run now", you want a durable queue with a worker — and
the [outbox](outbox.md) is how work gets into one without losing it.

## Next

- [Distributed locks](locks.md) · [Lifecycle](lifecycle.md)

The dedicated runnable example is [`examples/scheduling`](../examples/scheduling).
