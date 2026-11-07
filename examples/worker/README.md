# worker

A service with **no API**. It serves only the health endpoints; everything
else it does happens on a schedule.

```bash
go run ./worker

curl localhost:8082/healthz
curl localhost:8082/health
```

Then press Ctrl-C and read the last few lines.

## Why this example exists

It is easy to conclude from the other examples that a module is a REST
resource. It is not. A module owns dependencies, a lifecycle and its own
health; routes are optional.

```go
return &Module{
	Base: module.NewBase("reports",
		// No controllers. The module's whole surface is its schedule.
		module.WithHealthChecks(...),
	),
}
```

## Things worth trying

| Try this | And see |
|---|---|
| Watch for ~15 seconds | Two schedules, each run with its own trace id |
| Wait for a failure | The schedule continues — a failing job is logged, not fatal |
| Press Ctrl-C mid-run | The run finishes under the drain deadline, then `OnShutdown` |
| Break the cron expression in `reports_module.go` | The process refuses to start |

That last one is the point of validating at registration: a malformed
expression fails the deploy rather than the night it was supposed to run.

## Jobs

```go
sched.MustAdd(scheduler.Job{
	Name:       "reports.daily",
	Every:      5 * time.Second,   // a real one would be "0 2 * * *"
	Singleton:  true,              // one replica, via the locker
	Timeout:    30 * time.Second,
	Jitter:     time.Second,
	RunOnStart: true,
	Run:        svc.BuildDaily,
})
```

`Singleton` uses an in-memory locker here because this is one process. Swap
in `lock.NewRedis` and the same job runs on exactly one of N replicas;
nothing else changes.

`BuildDaily` honours its context between units of work. A job that ignores
its deadline cannot be stopped — shutdown waits for it until the drain
timeout and then abandons it mid-write.

## Where to look

| | |
|---|---|
| [`reports_module.go`](apps/jobs/reports/reports_module.go) | A module with no routes, and its schedules |
| [`service/report_service.go`](apps/jobs/reports/service/report_service.go) | Ordinary methods taking a context — testable without a scheduler |
| [`main.go`](main.go) | The locker, the lifecycle registration, the drain timeout |
