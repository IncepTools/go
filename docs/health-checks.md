# Health checks

`web/health` answers the two questions an orchestrator asks, which are **not
the same question**.

| Endpoint | Question | Checks |
|---|---|---|
| `GET /livez` | Is this process broken beyond recovery? | **Nothing.** |
| `GET /healthz` | Compatibility alias for liveness. | **Nothing.** |
| `GET /readyz` | Can it serve traffic right now? | Dependencies. |
| `GET /health` | What is the state of everything? | Dependencies, with latency. |

`inceptools.New` mounts all three outside the version prefix, so a probe does
not have to be updated when the API is versioned.

## Why liveness checks nothing

Conflating the two is the classic outage: a liveness probe that pings the
database means a database blip **restarts every replica simultaneously**,
turning a recoverable dependency failure into a total one.

If `/livez` can answer, the process is alive. Point the liveness probe there
and nowhere else. `/healthz` remains as a compatibility alias.

```yaml
livenessProbe:
  httpGet: { path: /livez, port: 8080 }
  periodSeconds: 10
readinessProbe:
  httpGet: { path: /readyz, port: 8080 }
  periodSeconds: 5
```

## Registering checks

In a module:

```go
module.NewBase("products",
	module.WithControllers(ctrl),
	module.WithHealthChecks(
		health.Of("database", health.Critical, pingDB),
		health.Of("cache", health.Optional, c.Ping),
	),
)
```

Or directly:

```go
app.Health.Register(health.Of("broker", health.Optional, broker.Ping))
```

## Severity is the decision that matters

| | |
|---|---|
| `health.Critical` | Failing takes the service out of rotation. |
| `health.Optional` | Failing degrades it; it keeps serving. |

Most caches, most third-party APIs and every analytics sink are **Optional**.
A service that reports itself `DOWN` because an analytics endpoint is slow
has made an outage out of a nuisance.

Ask: *can this service do anything useful without it?* Usually: without the
database, no — `Critical`. Without the cache, yes but slower — `Optional`.

## Statuses

| Report | `/readyz` | |
|---|---|---|
| `UP` | 200 | Everything fine. |
| `DEGRADED` | **200** | An optional dependency is down. Keep serving. |
| `DOWN` | 503 | A critical dependency is down. |

## Writing a check

```go
health.Of("database", health.Critical, func(ctx context.Context) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
})
```

**Honour the context.** Each check gets a deadline (2s by default); a check
that ignores it holds up the whole report — which is how a readiness endpoint
times out and takes every replica out of rotation at once.

**Keep it cheap.** `/readyz` is polled every few seconds by every replica. A
`SELECT 1` is right; a count over a large table is not.

**Check reachability, not correctness.** "Can I reach the database" is a
health check. "Is the data consistent" is a different job on a different
schedule.

Checks run **concurrently**. Sequentially, six two-second checks take twelve
seconds and get killed by the probe timeout they were meant to satisfy.

A panicking check is contained and reported as a failure — the endpoint
exists to report trouble, not to become it.

## The detailed report

```json
{
  "status": "DEGRADED",
  "service": "shop",
  "version": "1.4.2",
  "uptime": "3h12m4s",
  "checks": {
    "database": { "name": "database", "status": "UP", "latencyMs": 2 },
    "cache":    { "name": "cache", "status": "DEGRADED", "latencyMs": 2001,
                  "message": "context deadline exceeded", "severity": "optional" }
  }
}
```

**Put `/health` behind authentication.** The check names and messages
describe your internal topology.

## Startup

A slow-starting service needs a startup probe as well, or the liveness probe
kills it before it is ready:

```yaml
startupProbe:
  httpGet: { path: /livez, port: 8080 }
  failureThreshold: 30
  periodSeconds: 2
```

## Shutdown

On `SIGTERM` the listener stops accepting new connections and in-flight
requests drain. Give the load balancer time to notice by sleeping briefly in
a `preStop` hook — otherwise it keeps routing to a socket that is already
closing.

See [Lifecycle](lifecycle.md).

## Next

- [Lifecycle](lifecycle.md) · [Resilience](resilience.md)
