// Package cache is a small, typed caching layer with two implementations and
// one rule: a cache failure is never a request failure.
//
// Every method here degrades to a miss. A Redis that is down makes the service
// slower; it must not make it broken. That is the single most important
// property of a cache wrapper and the one most hand-rolled ones get wrong, by
// returning the Redis error up the stack and turning a latency problem into a
// 500.
package cache

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/IncepTools/go/src/core/logx"
)

// ErrMiss is returned by Get when the key is absent. It is the only error a
// caller normally handles; everything else is logged and reported as a miss.
var ErrMiss = errors.New("cache: miss")

// Cache is the storage contract.
//
// An implementation MUST treat a backend failure on Get as a miss, and a
// backend failure on Set as a no-op. It should log; it must not propagate.
type Cache interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, keys ...string) error
	// DeletePrefix removes every key under a prefix. On Redis this is a SCAN,
	// not a KEYS: KEYS blocks the server for the length of the keyspace, and
	// on a production instance that is an outage.
	DeletePrefix(ctx context.Context, prefix string) error
	Ping(ctx context.Context) error
}

// Key builds a namespaced cache key from parts: Key("catalog", "product", id)
// gives "catalog:product:<id>".
//
// Always namespace. An un-namespaced key collides across modules, and the
// collision presents as one module reading another's value with the right
// type and the wrong meaning — which is far harder to find than a crash.
func Key(parts ...string) string { return strings.Join(parts, ":") }

// Get reads and decodes a value.
func Get[T any](ctx context.Context, c Cache, key string) (T, bool) {
	var zero T
	if c == nil {
		return zero, false
	}
	raw, err := c.Get(ctx, key)
	if err != nil || len(raw) == 0 {
		return zero, false
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		// A value that will not decode is a stale shape from an older
		// deployment. Drop it rather than failing: the next write replaces it
		// and nobody has to flush a cache during a rollout.
		logx.C(ctx).Debug("cache value could not be decoded; treating as a miss", "key", key)
		_ = c.Delete(ctx, key)
		return zero, false
	}
	return out, true
}

// Set encodes and stores a value. A nil cache or a failing backend is a no-op.
func Set[T any](ctx context.Context, c Cache, key string, value T, ttl time.Duration) {
	if c == nil {
		return
	}
	raw, err := json.Marshal(value)
	if err != nil {
		logx.C(ctx).Warn("cache value could not be encoded", "key", key, "error", err.Error())
		return
	}
	_ = c.Set(ctx, key, raw, ttl)
}

// GetOrSet returns the cached value, or calls load, stores the result and
// returns it.
//
// load's error is returned to the caller UNCACHED. Caching a failure is
// occasionally what you want — see Negative below — but it must be a decision,
// never a default: a cached 500 outlives the incident that caused it.
func GetOrSet[T any](ctx context.Context, c Cache, key string, ttl time.Duration, load func(ctx context.Context) (T, error)) (T, error) {
	if v, ok := Get[T](ctx, c, key); ok {
		return v, nil
	}
	v, err := load(ctx)
	if err != nil {
		var zero T
		return zero, err
	}
	Set(ctx, c, key, v, ttl)
	return v, nil
}

// Memory is an in-process cache with TTL expiry.
//
// It is correct for a single instance, for development, and for data that is
// genuinely per-process. It is NOT shared: with N replicas you have N caches
// that invalidate independently, so a Delete on one leaves the other N-1
// serving the old value until it expires. Where that matters, use Redis.
type Memory struct {
	mu    sync.RWMutex
	items map[string]memItem
	done  chan struct{}
	once  sync.Once
}

type memItem struct {
	value     []byte
	expiresAt time.Time
}

// NewMemory returns an in-process cache that sweeps expired entries every
// interval (default 1 minute). Call Close to stop the sweeper.
func NewMemory(interval time.Duration) *Memory {
	if interval <= 0 {
		interval = time.Minute
	}
	m := &Memory{items: map[string]memItem{}, done: make(chan struct{})}
	go m.sweep(interval)
	return m
}

func (m *Memory) sweep(interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-m.done:
			return
		case now := <-t.C:
			m.mu.Lock()
			for k, it := range m.items {
				if !it.expiresAt.IsZero() && now.After(it.expiresAt) {
					delete(m.items, k)
				}
			}
			m.mu.Unlock()
		}
	}
}

// Get implements Cache.
func (m *Memory) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.RLock()
	it, ok := m.items[key]
	m.mu.RUnlock()
	if !ok {
		return nil, ErrMiss
	}
	if !it.expiresAt.IsZero() && time.Now().After(it.expiresAt) {
		m.mu.Lock()
		delete(m.items, key)
		m.mu.Unlock()
		return nil, ErrMiss
	}
	return it.value, nil
}

// Set implements Cache.
func (m *Memory) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	var exp time.Time
	if ttl > 0 {
		exp = time.Now().Add(ttl)
	}
	m.mu.Lock()
	m.items[key] = memItem{value: value, expiresAt: exp}
	m.mu.Unlock()
	return nil
}

// Delete implements Cache.
func (m *Memory) Delete(_ context.Context, keys ...string) error {
	m.mu.Lock()
	for _, k := range keys {
		delete(m.items, k)
	}
	m.mu.Unlock()
	return nil
}

// DeletePrefix implements Cache.
func (m *Memory) DeletePrefix(_ context.Context, prefix string) error {
	m.mu.Lock()
	for k := range m.items {
		if strings.HasPrefix(k, prefix) {
			delete(m.items, k)
		}
	}
	m.mu.Unlock()
	return nil
}

// Ping implements Cache.
func (m *Memory) Ping(context.Context) error { return nil }

// Close stops the sweeper. Safe to call more than once.
func (m *Memory) Close() error {
	m.once.Do(func() { close(m.done) })
	return nil
}

// Noop is a cache that stores nothing. Inject it to disable caching without
// making every call site nil-check.
type Noop struct{}

// Get implements Cache.
func (Noop) Get(context.Context, string) ([]byte, error) { return nil, ErrMiss }

// Set implements Cache.
func (Noop) Set(context.Context, string, []byte, time.Duration) error { return nil }

// Delete implements Cache.
func (Noop) Delete(context.Context, ...string) error { return nil }

// DeletePrefix implements Cache.
func (Noop) DeletePrefix(context.Context, string) error { return nil }

// Ping implements Cache.
func (Noop) Ping(context.Context) error { return nil }
