# scheduling

Singleton scheduled jobs, with a way to ask how they are doing.

```bash
go run ./scheduling

curl localhost:8086/api/v1/jobs/reports                  # every job's last run
curl localhost:8086/api/v1/jobs/reports/reports.daily    # one job
curl localhost:8086/api/v1/jobs/reports/nope             # 404, typed
```

Then stop it with Ctrl-C and watch the run in flight finish before the process
exits.

## Layout

```
apps/jobs/                      the domain
  jobs_app.go                   Descriptor, Providers (the engine), OnReady, OnShutdown
  reports/                      the module
    reports_module.go           Providers, New — and the schedule itself
    constants/                  module name, job names, messages
    dto/                        RunStatus
    service/                    the work, and the record of what it did
    controller/                 the HTTP surface
```

The schedule lives in the module, next to the work it runs. A job declared in
`main` is a job nobody finds when they open the module that owns it — and when
the module moves to another service, its schedule is left behind.

The scheduler *engine* is one level up, in the domain, because every module in
the domain adds jobs to the same one. Two engines would mean two sets of
singleton locks, and a job declared on one would be invisible to the other.

## The four fields that matter

**`Singleton`** — exactly one replica runs it. Without the lock every replica
builds the same report at the same instant: wasted work at best, duplicate
side effects as soon as the job sends anything. `reports.cleanup` is *not* a
singleton, deliberately: removing expired rows is idempotent and local, so
there is nothing to protect.

**`Timeout`** — bounds a run. A singleton job with no timeout and a hung
dependency holds its lock forever, and the job never runs again.

**`Jitter`** — spreads replicas that all woke on the same tick, so they do not
contend for the lock at the same moment.

**`RunOnStart`** — runs once at boot rather than waiting for the first tick.

## Status is not optional

A scheduled job with no way to ask how it is doing is a job that fails
unnoticed. The service records each run's outcome and the controller exposes
it, which is why this example has an HTTP surface at all.

The memory locker suits one process; several replicas need a distributed one,
and no module changes. See
[`docs/task-scheduling.md`](../../docs/task-scheduling.md).
