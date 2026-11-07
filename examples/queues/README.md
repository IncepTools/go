# queues

An HTTP endpoint that accepts work, and workers that retry it.

```bash
go run ./queues

curl -X POST localhost:8085/api/v1/queues/jobs \
  -H 'Content-Type: application/json' \
  -d '{"kind":"report","subject":"october"}'

# Watch three attempts and then the dead-letter handler:
curl -X POST localhost:8085/api/v1/queues/jobs \
  -H 'Content-Type: application/json' \
  -d '{"kind":"export","subject":"doomed","fail":true}'

# Refused at the edge, before anything is enqueued:
curl -X POST localhost:8085/api/v1/queues/jobs \
  -H 'Content-Type: application/json' -d '{"kind":"nope","subject":"x"}'
```

## Layout

```
apps/queues/                    the domain
  queues_app.go                 Descriptor, Providers, NewApp
  jobs/                         the module
    jobs_module.go              Providers (builds the queue), New, OnReady, OnShutdown
    constants/                  module and queue names, messages
    dto/                        SubmitJob, JobReceipt
    service/                    JobService (submits), Processor (handles)
    controller/                 the HTTP surface
```

The queue is built in the module's `Providers`, and the module's `OnReady` and
`OnShutdown` start and drain it. A mounted module is a lifecycle participant,
so that is the entire registration — there is no second step in `main` that
is easy to forget and silently drops in-flight jobs when it is.

## Why the service is two types

`Processor` does the work and needs no queue. `JobService` submits and needs
the queue. One type cannot be both, because the queue is constructed *with*
its handler — the handler cannot already hold the thing it is being passed to.
The split follows the dependency direction rather than fighting it.

## What to take from it

**Validate at the edge.** The `oneof` rule rejects a bad `kind` before the job
is durable. A queue is the worst place to discover a payload was never going
to work: the worker fails, retries the same bytes, and dead-letters it.

**Handlers must be idempotent.** At-least-once delivery is the contract of
every queue worth using. A worker that crashes after doing the work but before
acknowledging it is enough to make this run twice.

**Returning an error asks for a retry.** A handler that swallows its errors
reports success for work that did not happen, and nothing retries it.

**A dead letter that is only logged is a dropped job.** It is logged here so
the path is visible; a real service persists the payload so it can be
inspected and replayed.

This queue is in-process: buffered work is lost on restart. See
[`docs/queues.md`](../../docs/queues.md) for backpressure and the outbox
boundary.
