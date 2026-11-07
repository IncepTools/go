# Queues example

```bash
go run ./queues
curl -X POST localhost:8085/api/v1/queues/jobs -d 'build docs'
```

The queue has two workers and retries failed jobs. Messages are acknowledged
by the handler and failed messages go through the configured retry policy.
This example is intentionally in-process: buffered work is lost on restart.

See [`docs/queues.md`](../../docs/queues.md) for backpressure, dead letters,
shutdown draining and the outbox boundary.
