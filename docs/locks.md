# Distributed locks

`platform/lock` is mutual exclusion across processes: "only one replica runs
this at a time".

```go
ran, err := lock.Do(ctx, locker, "nightly-reconcile", 10*time.Minute,
	func(ctx context.Context) error {
		return svc.Reconcile(ctx)
	})
if err != nil {
	return err
}
if !ran {
	// Another replica has it. Not an error.
}
```

## The four properties

Every correct distributed lock has these, and the naive implementation
(`SETNX`, then `DEL`) has two:

1. **A TTL**, so a crashed holder does not hold the lock forever.
2. **A unique token**, so release only releases *your* lock. Without it, a
   holder whose TTL expired mid-work deletes the lock a different replica has
   since acquired — and then two replicas are inside the critical section.
3. **A compare-and-delete release**, done in Lua, because checking the token
   and deleting it as two commands is the same race one layer down.
4. **Optional refresh**, for work that may outlive the TTL.

## What it is not

**A correctness guarantee.** If the Redis primary dies after granting a lock
and before replicating it, the replica can grant it again.

Use this to prevent duplicate **work**, not to protect an **invariant**. For
invariants — a balance, a stock count, an allocation — use a database
transaction with a row lock:

```go
repo.Transaction(ctx, db, func(ctx context.Context) error {
	item, err := r.LockByID(ctx, id)     // SELECT ... FOR UPDATE
	...
})
```

The difference matters. "Two replicas both sent the weekly digest" is
annoying. "Two replicas both decremented the same stock" is wrong.

## Implementations

```go
lock.NewRedis(client, "shop")   // across replicas
lock.NewMemory()                // one process; tests and development
lock.Noop{}                     // always granted
```

`lock.NewMemory` enforces nothing across replicas — each has its own map — so
do not reach for it to "get started" on work that will later run on more than
one.

`lock.Noop` is honest when exactly one replica runs the work by deployment.
Say so in a comment where you use it.

## Manual acquisition

```go
held, err := locker.Acquire(ctx, "import:"+fileID, time.Minute)
if errors.Is(err, lock.ErrNotAcquired) {
	return errs.Conflict("this import is already running").WithCode("IMPORT_RUNNING")
}
if err != nil {
	return err
}
defer held.Release(context.WithoutCancel(ctx))
```

Release on a context that cannot already be cancelled. If the work returned
because the request was cancelled, releasing with that same context fails,
and the lock sits until its TTL for no reason. `lock.Do` handles this for you.

## Long work

```go
go func() {
	t := time.NewTicker(ttl / 3)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			if err := held.Refresh(ctx, ttl); err != nil {
				cancel()   // we no longer hold it; another replica may be running
				return
			}
		}
	}
}()
```

`ErrNotHeld` from `Refresh` or `Release` means the TTL expired while the work
was still running. Treat it as a signal that the TTL is too short, not as a
nuisance — at that moment another replica may already be doing the same work.

## Naming keys

Namespace by what is being protected, and include the identifier where the
lock is per-thing:

```
"job:nightly-reconcile"
"import:9c1e…"
"account:1a3c…:rebuild-index"
```

The Redis locker prefixes with the service name you pass, so two services
cannot take each other's locks.

## Choosing a TTL

Longer than the worst-case runtime, shorter than the time you are willing to
wait after a crash. For a ten-minute job, fifteen minutes with refresh is
reasonable. Too short is the dangerous direction: the lock expires mid-work
and a second worker starts.

## Next

- [Task scheduling](task-scheduling.md) — the main consumer.
- [Database](database.md) — row locks, for invariants.
