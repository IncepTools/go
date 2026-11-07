package middleware

import (
	"strconv"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/IncepTools/go/src/core/errs"
	"github.com/IncepTools/go/src/core/logx"
	"github.com/IncepTools/go/src/web/response"
)

// RateLimitStore counts requests per key. The in-process implementation below
// is correct for a single instance; behind more than one replica, back it with
// Redis so the limit is the limit rather than the limit times the replica
// count.
type RateLimitStore interface {
	// Incr adds one to key's count within window and returns the new count
	// and the time the window resets.
	Incr(key string, window time.Duration) (count int, resetAt time.Time, err error)
}

// RateLimitOptions configures RateLimit.
type RateLimitOptions struct {
	// Max requests per Window. Zero disables the middleware entirely, which
	// is the honest behaviour for an unset config — better than a limit of
	// zero that refuses everything.
	Max    int
	Window time.Duration
	// KeyFunc identifies the caller. The default is the client IP, which is
	// the right default and the wrong one behind a proxy that does not set
	// X-Forwarded-For: everyone shares one key and one user exhausts it for
	// all. Prefer an account or api-key based function.
	KeyFunc func(c fiber.Ctx) string
	// Skip exempts a request — a health probe, an internal caller.
	Skip  func(c fiber.Ctx) bool
	Store RateLimitStore
	// FailOpen decides what happens when the store errors. True (the default)
	// serves the request: a Redis blip should not take the API down with it.
	// Set false where the limit protects something that must not be exceeded.
	FailOpen *bool
}

// RateLimit refuses callers over their budget with 429 and the standard
// RateLimit-* headers so a well-behaved client can back off instead of
// hammering.
func RateLimit(opt RateLimitOptions) fiber.Handler {
	if opt.Max <= 0 || opt.Window <= 0 {
		return func(c fiber.Ctx) error { return c.Next() }
	}
	if opt.KeyFunc == nil {
		opt.KeyFunc = func(c fiber.Ctx) string { return c.IP() }
	}
	if opt.Store == nil {
		opt.Store = NewMemoryRateLimitStore()
	}
	failOpen := true
	if opt.FailOpen != nil {
		failOpen = *opt.FailOpen
	}

	return func(c fiber.Ctx) error {
		if opt.Skip != nil && opt.Skip(c) {
			return c.Next()
		}
		key := opt.KeyFunc(c)
		count, resetAt, err := opt.Store.Incr(key, opt.Window)
		if err != nil {
			logx.C(c.Context()).Warn("rate limit store failed", "error", err.Error(), "failOpen", failOpen)
			if failOpen {
				return c.Next()
			}
			return response.Fail(c, errs.Unavailable("rate limiting is unavailable").WithCode("RATE_LIMIT_UNAVAILABLE"))
		}

		remaining := max(opt.Max-count, 0)
		c.Set("RateLimit-Limit", strconv.Itoa(opt.Max))
		c.Set("RateLimit-Remaining", strconv.Itoa(remaining))
		c.Set("RateLimit-Reset", strconv.Itoa(int(time.Until(resetAt).Seconds())))

		if count > opt.Max {
			c.Set("Retry-After", strconv.Itoa(max(int(time.Until(resetAt).Seconds()), 1)))
			return response.Fail(c, errs.Exhausted("too many requests; retry after %ds",
				max(int(time.Until(resetAt).Seconds()), 1)).WithCode("RATE_LIMITED"))
		}
		return c.Next()
	}
}

type memoryBucket struct {
	count   int
	resetAt time.Time
}

// MemoryRateLimitStore is a fixed-window counter in process memory.
//
// It is correct for one instance and for development. It is NOT a distributed
// limiter: with N replicas the effective limit is N times the configured one,
// and the counts vanish on restart.
type MemoryRateLimitStore struct {
	mu      sync.Mutex
	buckets map[string]*memoryBucket
	// lastSweep bounds the map: without eviction, a per-IP limiter is an
	// unbounded map keyed by attacker-controlled input, which is a slow leak
	// with a hostile author.
	lastSweep time.Time
}

// NewMemoryRateLimitStore returns an in-process store.
func NewMemoryRateLimitStore() *MemoryRateLimitStore {
	return &MemoryRateLimitStore{buckets: map[string]*memoryBucket{}, lastSweep: time.Now()}
}

// Incr implements RateLimitStore.
func (s *MemoryRateLimitStore) Incr(key string, window time.Duration) (int, time.Time, error) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

	if now.Sub(s.lastSweep) > window {
		for k, b := range s.buckets {
			if now.After(b.resetAt) {
				delete(s.buckets, k)
			}
		}
		s.lastSweep = now
	}

	b, ok := s.buckets[key]
	if !ok || now.After(b.resetAt) {
		b = &memoryBucket{resetAt: now.Add(window)}
		s.buckets[key] = b
	}
	b.count++
	return b.count, b.resetAt, nil
}
