package cache

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/IncepTools/go/core/logx"
)

// Redis is a shared cache backed by a Redis server.
type Redis struct {
	client redis.UniversalClient
	// Prefix namespaces every key, so two services sharing one Redis cannot
	// collide. Set it to the service name.
	prefix string
}

// NewRedis wraps a client. prefix is prepended to every key.
func NewRedis(client redis.UniversalClient, prefix string) *Redis {
	if prefix != "" && prefix[len(prefix)-1] != ':' {
		prefix += ":"
	}
	return &Redis{client: client, prefix: prefix}
}

func (r *Redis) k(key string) string { return r.prefix + key }

// Get implements Cache. Every failure other than a genuine miss is logged and
// reported as a miss, so a Redis outage degrades latency rather than
// availability.
func (r *Redis) Get(ctx context.Context, key string) ([]byte, error) {
	v, err := r.client.Get(ctx, r.k(key)).Bytes()
	switch {
	case err == nil:
		return v, nil
	case errors.Is(err, redis.Nil):
		return nil, ErrMiss
	default:
		logx.C(ctx).Warn("cache read failed; serving as a miss", "key", key, "error", err.Error())
		return nil, ErrMiss
	}
}

// Set implements Cache. A failure is logged and swallowed.
func (r *Redis) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := r.client.Set(ctx, r.k(key), value, ttl).Err(); err != nil {
		logx.C(ctx).Warn("cache write failed", "key", key, "error", err.Error())
	}
	return nil
}

// Delete implements Cache.
//
// Unlike Get and Set, a delete failure is RETURNED. An invalidation that
// silently did not happen leaves the cache serving data the database no
// longer has, and the caller may want to refuse the write rather than accept
// that.
func (r *Redis) Delete(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	full := make([]string, len(keys))
	for i, k := range keys {
		full[i] = r.k(k)
	}
	return r.client.Del(ctx, full...).Err()
}

// DeletePrefix removes every key under a prefix using SCAN in batches.
//
// SCAN, never KEYS: KEYS is O(keyspace) and blocks the single-threaded server
// for its duration, which on a production instance is an outage caused by a
// cache invalidation.
func (r *Redis) DeletePrefix(ctx context.Context, prefix string) error {
	pattern := r.k(prefix) + "*"
	var cursor uint64
	for {
		keys, next, err := r.client.Scan(ctx, cursor, pattern, 256).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			if err := r.client.Del(ctx, keys...).Err(); err != nil {
				return err
			}
		}
		if next == 0 {
			return nil
		}
		cursor = next
	}
}

// Ping implements Cache.
func (r *Redis) Ping(ctx context.Context) error { return r.client.Ping(ctx).Err() }

// Close releases the client.
func (r *Redis) Close() error { return r.client.Close() }
