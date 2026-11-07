# Caching

`platform/cache` is a small typed caching layer with two implementations and
one rule: **a cache failure is never a request failure.**

Every method degrades to a miss. A Redis that is down makes the service
slower; it must not make it broken. That is the most important property of a
cache wrapper and the one hand-rolled ones usually get wrong, by returning
the Redis error up the stack and turning a latency problem into a 500.

## Using it

```go
product, err := cache.GetOrSet(ctx, c, cache.Key("catalog", "product", id.String()),
	5*time.Minute,
	func(ctx context.Context) (dto.ProductResponse, error) {
		return svc.Get(ctx, id)
	})
```

Or the two halves separately:

```go
if v, ok := cache.Get[dto.ProductResponse](ctx, c, key); ok {
	return v, nil
}
v, err := load(ctx)
if err != nil {
	return v, err
}
cache.Set(ctx, c, key, v, 5*time.Minute)
```

## Implementations

```go
cache.NewMemory(time.Minute)           // in-process, with a TTL sweeper
cache.NewRedis(client, "shop")         // shared, namespaced
cache.Noop{}                           // caching off, no nil checks at call sites
```

**Memory** is correct for a single instance, for development, and for data
that is genuinely per-process. It is *not* shared: with N replicas you have N
caches that invalidate independently, so a `Delete` on one leaves the other
N−1 serving the old value until it expires.

**Redis** is namespaced by the prefix you pass, so two services sharing one
instance cannot collide.

Register it under the interface, so a test can substitute:

```go
di.MustSupply[cache.Cache](c, cache.NewRedis(client, "shop"))
```

## Keys

```go
cache.Key("catalog", "product", id.String())   // "catalog:product:<id>"
```

**Always namespace, and always include the account** where the data is
per-account:

```go
cache.Key("catalog", acct.String(), "product", id.String())
```

A key without the account is one customer's data served to another — the same
failure the repository's scoping prevents, moved one layer up and much harder
to notice. An un-namespaced key collides across modules, and the collision
presents as one module reading another's value with the right type and the
wrong meaning.

## Invalidation

```go
_ = c.Delete(ctx, key)
_ = c.DeletePrefix(ctx, cache.Key("catalog", acct.String())+":")
```

`Delete` **returns** its error, unlike `Get` and `Set`. An invalidation that
silently did not happen leaves the cache serving data the database no longer
has, and the caller may want to refuse the write rather than accept that.

On Redis, `DeletePrefix` is a `SCAN`, never `KEYS`. `KEYS` is O(keyspace) and
blocks the single-threaded server for its duration — an outage caused by a
cache invalidation.

## Failures are not cached

```go
for range 3 {
	_, err := cache.GetOrSet(ctx, c, key, ttl, failingLoad)   // loads 3 times
}
```

Caching a failure is occasionally what you want — a negative lookup against an
expensive upstream — but it must be a decision, never a default: a cached 500
outlives the incident that caused it.

Do it explicitly when you mean it:

```go
type lookup struct {
	Found   bool
	Product dto.ProductResponse
}
```

## Decoding failures are misses

A value that will not decode is a stale shape from an older deployment. It is
dropped and treated as a miss, so the next write replaces it and nobody has
to flush a cache during a rollout.

## What to cache, and for how long

| | |
|---|---|
| Reference data that rarely changes | minutes to hours |
| A per-account computed view | seconds to a minute, invalidated on write |
| An expensive upstream response | as long as the upstream's own freshness allows |
| Anything a user is about to edit | **do not** — they will see their own write disappear |

A short TTL *and* explicit invalidation is the usual right answer. The TTL
bounds the damage when an invalidation is missed, which it eventually will be.

## Caching a whole response

An [interceptor](interceptors.md) can skip the handler on a hit. Be careful
that the key covers everything that varies the response: the account, the
query string, the `Accept-Language`. A response cached on the path alone is
the leak described above.

## Health

```go
module.WithHealthChecks(
	health.Of("cache", health.Optional, c.Ping),
)
```

`Optional`, because a cache being down degrades the service rather than
breaking it. Marking it `Critical` means a Redis blip takes every replica out
of rotation — turning a latency problem into an outage, which is exactly what
this package is designed to prevent.

## Next

- [Interceptors](interceptors.md) · [Health checks](health-checks.md)
