package cache_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/IncepTools/go/src/platform/cache"
)

type user struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func TestMemoryRoundTrip(t *testing.T) {
	c := cache.NewMemory(time.Minute)
	defer c.Close()
	ctx := context.Background()

	cache.Set(ctx, c, "u:1", user{ID: "1", Name: "ada"}, time.Minute)
	got, ok := cache.Get[user](ctx, c, "u:1")
	if !ok || got.Name != "ada" {
		t.Fatalf("got = %+v, ok = %v", got, ok)
	}
}

func TestExpiry(t *testing.T) {
	c := cache.NewMemory(time.Minute)
	defer c.Close()
	ctx := context.Background()

	cache.Set(ctx, c, "k", "v", 10*time.Millisecond)
	time.Sleep(30 * time.Millisecond)
	if _, ok := cache.Get[string](ctx, c, "k"); ok {
		t.Fatal("an expired entry was served")
	}
}

func TestUndecodableValueIsAMissNotAnError(t *testing.T) {
	// The real case: a deployment changes the cached struct's shape, and the
	// old values must not break the new code.
	c := cache.NewMemory(time.Minute)
	defer c.Close()
	ctx := context.Background()

	_ = c.Set(ctx, "k", []byte(`{"id": ["not", "a", "string"]}`), time.Minute)
	if _, ok := cache.Get[user](ctx, c, "k"); ok {
		t.Fatal("a value of the wrong shape was returned as a hit")
	}
	if _, err := c.Get(ctx, "k"); !errors.Is(err, cache.ErrMiss) {
		t.Fatal("the undecodable value was not dropped")
	}
}

func TestGetOrSetDoesNotCacheFailures(t *testing.T) {
	c := cache.NewMemory(time.Minute)
	defer c.Close()
	ctx := context.Background()

	calls := 0
	load := func(context.Context) (string, error) {
		calls++
		return "", errors.New("upstream down")
	}
	for range 3 {
		if _, err := cache.GetOrSet(ctx, c, "k", time.Minute, load); err == nil {
			t.Fatal("expected the load error")
		}
	}
	if calls != 3 {
		t.Fatalf("calls = %d; a failure must not be cached", calls)
	}
}

func TestGetOrSetCachesSuccess(t *testing.T) {
	c := cache.NewMemory(time.Minute)
	defer c.Close()
	ctx := context.Background()

	calls := 0
	load := func(context.Context) (string, error) { calls++; return "value", nil }
	for range 3 {
		got, err := cache.GetOrSet(ctx, c, "k", time.Minute, load)
		if err != nil || got != "value" {
			t.Fatalf("got = %q, err = %v", got, err)
		}
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestDeletePrefix(t *testing.T) {
	c := cache.NewMemory(time.Minute)
	defer c.Close()
	ctx := context.Background()

	cache.Set(ctx, c, "catalog:product:1", 1, time.Minute)
	cache.Set(ctx, c, "catalog:product:2", 2, time.Minute)
	cache.Set(ctx, c, "billing:invoice:1", 3, time.Minute)

	if err := c.DeletePrefix(ctx, "catalog:"); err != nil {
		t.Fatal(err)
	}
	if _, ok := cache.Get[int](ctx, c, "catalog:product:1"); ok {
		t.Fatal("prefix delete missed a key")
	}
	if _, ok := cache.Get[int](ctx, c, "billing:invoice:1"); !ok {
		t.Fatal("prefix delete removed an unrelated namespace")
	}
}

func TestNilCacheIsSafe(t *testing.T) {
	ctx := context.Background()
	cache.Set[string](ctx, nil, "k", "v", time.Minute)
	if _, ok := cache.Get[string](ctx, nil, "k"); ok {
		t.Fatal("a nil cache reported a hit")
	}
}

func TestKey(t *testing.T) {
	if got := cache.Key("catalog", "product", "abc"); got != "catalog:product:abc" {
		t.Fatalf("got %q", got)
	}
}
