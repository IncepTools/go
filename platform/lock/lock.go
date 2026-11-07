// Package lock is mutual exclusion across processes: "only one replica runs
// this at a time".
//
// Every correct distributed lock has four properties, and the naive
// implementation (SETNX, then DEL) has two of them. This one has all four:
//
//  1. A TTL, so a crashed holder does not hold the lock forever.
//  2. A unique token, so release only releases YOUR lock. Without it, a holder
//     whose TTL expired mid-work deletes the lock a different replica has since
//     acquired, and then two replicas are inside the critical section.
//  3. A compare-and-delete release, done in Lua, because checking the token
//     and deleting it as two commands is the same race one layer down.
//  4. Optional refresh, for work that may outlive the TTL.
//
// What it is NOT: a correctness guarantee against a Redis failover. If the
// primary dies after granting a lock and before replicating it, the replica
// can grant it again. Use this to prevent duplicate WORK, not to protect an
// invariant — for invariants, use a database transaction with a row lock.
package lock

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// ErrNotAcquired is returned when the lock is held by somebody else.
var ErrNotAcquired = errors.New("lock: already held")

// ErrNotHeld is returned when releasing or refreshing a lock this process no
// longer owns — which means its TTL expired while the work was still running.
// Treat it as a signal that the TTL is too short, not as a nuisance.
var ErrNotHeld = errors.New("lock: no longer held")

// Lock is one acquired lock.
type Lock struct {
	Key   string
	Token string

	release func(ctx context.Context) error
	refresh func(ctx context.Context, ttl time.Duration) error
}

// Release gives the lock up. It is safe to call on an expired lock; that
// returns ErrNotHeld and deletes nothing.
func (l *Lock) Release(ctx context.Context) error {
	if l == nil || l.release == nil {
		return nil
	}
	return l.release(ctx)
}

// Refresh extends the TTL. Call it periodically from long work, and stop the
// work if it ever returns ErrNotHeld — at that point another replica may
// already be running it.
func (l *Lock) Refresh(ctx context.Context, ttl time.Duration) error {
	if l == nil || l.refresh == nil {
		return ErrNotHeld
	}
	return l.refresh(ctx, ttl)
}

// Locker acquires locks.
type Locker interface {
	// Acquire takes the lock, or returns ErrNotAcquired immediately.
	Acquire(ctx context.Context, key string, ttl time.Duration) (*Lock, error)
}

// Do runs fn while holding key, and does nothing when the lock is held
// elsewhere.
//
// The common use: a cron job running on three replicas where exactly one
// should do the work. ran is false when another replica had it.
func Do(ctx context.Context, l Locker, key string, ttl time.Duration, fn func(ctx context.Context) error) (ran bool, err error) {
	lk, err := l.Acquire(ctx, key, ttl)
	if errors.Is(err, ErrNotAcquired) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	// Release on a context that cannot already be cancelled: if fn returned
	// because the request was cancelled, releasing with that same context
	// fails, and the lock sits until its TTL for no reason.
	defer func() {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = lk.Release(rctx)
	}()
	return true, fn(ctx)
}

// releaseScript deletes the key only when the value still matches our token.
const releaseScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
else
	return 0
end`

// refreshScript extends the TTL only when the value still matches our token.
const refreshScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("PEXPIRE", KEYS[1], ARGV[2])
else
	return 0
end`

// RedisLocker is a Redis-backed Locker.
type RedisLocker struct {
	client  redis.UniversalClient
	prefix  string
	release *redis.Script
	refresh *redis.Script
}

// NewRedis builds a locker. prefix namespaces the keys; set it to the service
// name so two services cannot take each other's locks.
func NewRedis(client redis.UniversalClient, prefix string) *RedisLocker {
	if prefix != "" && prefix[len(prefix)-1] != ':' {
		prefix += ":"
	}
	return &RedisLocker{
		client:  client,
		prefix:  prefix + "lock:",
		release: redis.NewScript(releaseScript),
		refresh: redis.NewScript(refreshScript),
	}
}

// Acquire implements Locker.
func (r *RedisLocker) Acquire(ctx context.Context, key string, ttl time.Duration) (*Lock, error) {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	full := r.prefix + key
	token := uuid.NewString()

	ok, err := r.client.SetNX(ctx, full, token, ttl).Result()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotAcquired
	}

	return &Lock{
		Key:   key,
		Token: token,
		release: func(ctx context.Context) error {
			n, err := r.release.Run(ctx, r.client, []string{full}, token).Int64()
			if err != nil {
				return err
			}
			if n == 0 {
				return ErrNotHeld
			}
			return nil
		},
		refresh: func(ctx context.Context, ttl time.Duration) error {
			n, err := r.refresh.Run(ctx, r.client, []string{full}, token, ttl.Milliseconds()).Int64()
			if err != nil {
				return err
			}
			if n == 0 {
				return ErrNotHeld
			}
			return nil
		},
	}, nil
}

// Memory is a single-process Locker.
//
// Correct for one instance and for tests. Across replicas it enforces nothing
// — every replica has its own map — so do not reach for it to "get started"
// on work that will later run on more than one.
type Memory struct {
	mu   sync.Mutex
	held map[string]memHeld
}

type memHeld struct {
	token     string
	expiresAt time.Time
}

// NewMemory returns an in-process locker.
func NewMemory() *Memory { return &Memory{held: map[string]memHeld{}} }

// Acquire implements Locker.
func (m *Memory) Acquire(_ context.Context, key string, ttl time.Duration) (*Lock, error) {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	now := time.Now()
	token := uuid.NewString()

	m.mu.Lock()
	if h, ok := m.held[key]; ok && now.Before(h.expiresAt) {
		m.mu.Unlock()
		return nil, ErrNotAcquired
	}
	m.held[key] = memHeld{token: token, expiresAt: now.Add(ttl)}
	m.mu.Unlock()

	return &Lock{
		Key:   key,
		Token: token,
		release: func(context.Context) error {
			m.mu.Lock()
			defer m.mu.Unlock()
			if h, ok := m.held[key]; ok && h.token == token {
				delete(m.held, key)
				return nil
			}
			return ErrNotHeld
		},
		refresh: func(_ context.Context, ttl time.Duration) error {
			m.mu.Lock()
			defer m.mu.Unlock()
			h, ok := m.held[key]
			if !ok || h.token != token {
				return ErrNotHeld
			}
			h.expiresAt = time.Now().Add(ttl)
			m.held[key] = h
			return nil
		},
	}, nil
}

// Noop grants every lock. Use it to run a scheduler on exactly one replica by
// deployment rather than by coordination.
type Noop struct{}

// Acquire implements Locker.
func (Noop) Acquire(context.Context, string, time.Duration) (*Lock, error) {
	return &Lock{Key: "noop", Token: "noop"}, nil
}
