# Scheduling example

```bash
go run ./scheduling
```

Runs a singleton report job every five seconds and drains it through the
application lifecycle.

The job runs once at startup and then every five seconds. It uses the memory
locker for this single-process example; production replicas should use a
distributed locker. See [`docs/task-scheduling.md`](../../docs/task-scheduling.md).
