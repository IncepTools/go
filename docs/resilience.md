# Resilience

Two packages for calling something that might not answer:
`reliability/retry` and `reliability/breaker`.

## Retry

```go
res, err := retry.Value(ctx, retry.Default(), func(ctx context.Context) (Quote, error) {
	return pricing.Fetch(ctx, sku)
})
```

`retry.Default()` is three attempts, 100ms growing to 2s, jittered.

### Retrying the wrong thing is worse than not retrying

- A retried POST that actually **succeeded** creates a second resource.
- A retry loop with **no jitter** turns a brief dependency blip into a
  synchronised stampede that keeps it down.
- A retry of a **400** is three requests that were always going to fail.

So this package defaults to **not** retrying, and asks you to say what is
retryable. `retry.Retryable` covers the usual answer:

| Kind | Retried? |
|---|---|
| `unavailable`, `timeout` | yes |
| `conflict` with code `CONCURRENT_MODIFICATION` | yes — two transactions raced |
| every other `conflict` | no — a uniqueness clash fails identically forever |
| `invalid`, `not_found`, `forbidden`, … | no |
| an **unclassified** error | **no** |

An unclassified error is not retried because nobody has decided what it
means, and it must not be assumed safe to repeat.

### The policy

```go
retry.Policy{
	MaxAttempts: 4,                      // counts the first try
	BaseDelay:   200 * time.Millisecond, // doubles each attempt
	MaxDelay:    5 * time.Second,
	Retryable:   func(err error) bool { ... },
	OnRetry:     func(attempt int, err error, wait time.Duration) { ... },
}
```

**Leave jitter on.** Without it, every client that failed at the same moment
retries at the same moment, and the dependency that was briefly slow is now
reliably overloaded.

The context is honoured between attempts: a cancelled caller stops
immediately rather than sleeping out the backoff.

### Only retry what is safe to repeat

A read is safe. A write is safe only if it is idempotent — which is what
[idempotency keys](idempotency.md) are for on the way in, and what a natural
key is for in the database.

## Circuit breaker

```go
pricing := breaker.New(breaker.Options{
	Name:      "pricing-api",
	Threshold: 5,
	Cooldown:  30 * time.Second,
})

quote, err := breaker.Value(ctx, pricing, func(ctx context.Context) (Quote, error) {
	return client.Fetch(ctx, sku)
})
```

### The problem it solves is not the failing dependency

It is what your service does about it. Every request waits for a three-second
timeout, so every worker is blocked waiting, so the service stops answering
the endpoints that do not need that dependency at all. One failing downstream
has taken out everything.

An open breaker fails those calls immediately with
`errs.Unavailable`/`CIRCUIT_OPEN`, which frees the workers and gives the
dependency room to recover.

### States

```
closed     calls pass; consecutive failures count toward the threshold
open       calls fail at once; after Cooldown, one probe is allowed
half-open  one call probes — success closes, failure re-opens
```

Exactly **one** probe at a time. Letting every waiting request through on the
cooldown boundary is a stampede onto a dependency that has not recovered yet.

### What counts as a failure

By default: `unavailable`, `timeout` and `internal`.

A 404 from a dependency is a perfectly healthy answer and must **not** open a
breaker — counting it means a client asking for missing things repeatedly
takes the dependency out for everyone.

```go
breaker.Options{
	Counts: func(err error) bool {
		return errs.Is(err, errs.KindUnavailable) || errs.Is(err, errs.KindTimeout)
	},
}
```

### One per dependency

Not one per service. A shared breaker lets an unrelated failure block a
healthy call.

### Observe transitions

```go
breaker.Options{
	OnStateChange: func(from, to breaker.State) {
		metrics.CircuitState.WithLabelValues("pricing-api").Set(stateValue(to))
	},
}
```

Called under the breaker's lock, so keep it cheap and do not call back into
the breaker.

## Using them together

Retry **inside** the breaker, not outside:

```go
quote, err := breaker.Value(ctx, pricing, func(ctx context.Context) (Quote, error) {
	return retry.Value(ctx, retry.Default(), func(ctx context.Context) (Quote, error) {
		return client.Fetch(ctx, sku)
	})
})
```

The breaker then sees one failure per logical call rather than three, so the
threshold means what it says. The other order makes the breaker open three
times faster than configured — and retries against an open breaker, which is
pointless.

## And a timeout

Always. A retry policy and a breaker around a call with no deadline is three
ways of waiting forever.

```go
ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
defer cancel()
```

## A client, assembled

```go
type PricingClient struct {
	http    *http.Client
	breaker *breaker.Breaker
	policy  retry.Policy
}

func (c *PricingClient) Quote(ctx context.Context, sku string) (Quote, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	return breaker.Value(ctx, c.breaker, func(ctx context.Context) (Quote, error) {
		return retry.Value(ctx, c.policy, func(ctx context.Context) (Quote, error) {
			return c.fetch(ctx, sku)
		})
	})
}

// fetch classifies the failure, which is what makes the rest of this work.
func (c *PricingClient) fetch(ctx context.Context, sku string) (Quote, error) {
	res, err := c.http.Do(req)
	if err != nil {
		return Quote{}, errs.Wrap(errs.KindUnavailable, "the pricing service is unreachable", err)
	}
	defer res.Body.Close()

	switch {
	case res.StatusCode == 404:
		return Quote{}, errs.NotFound("no price for %s", sku).WithCode("NO_PRICE")
	case res.StatusCode >= 500:
		return Quote{}, errs.Unavailable("the pricing service returned %d", res.StatusCode)
	case res.StatusCode >= 400:
		return Quote{}, errs.Internal("the pricing request was rejected: %d", res.StatusCode)
	}
	...
}
```

The classification in `fetch` is the load-bearing part. Retry and the breaker
both read the error's kind, so an unclassified error means neither does the
right thing.

## Degrading

Sometimes the correct answer to an open breaker is not an error:

```go
quote, err := c.Quote(ctx, sku)
if errs.CodeOf(err) == "CIRCUIT_OPEN" {
	return cachedOrDefault(sku), nil    // stale, and better than nothing
}
```

Decide that per call site. A stale price on a listing page is fine; a stale
price at checkout is not.

## Next

- [Caching](caching.md) — the fallback a degraded path usually reads.
- [Health checks](health-checks.md) — reporting a dependency that is down.
