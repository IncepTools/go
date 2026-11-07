package cache_test

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/IncepTools/go/src/platform/cache"
)

type product struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// GetOrSet is the usual shape: return the cached value, or load it, store
// it and return it.
func Example() {
	c := cache.NewMemory(time.Minute)
	defer c.Close() //nolint:errcheck

	ctx := context.Background()
	loads := 0
	load := func(context.Context) (product, error) {
		loads++
		return product{ID: "9c1e", Name: "Desk lamp"}, nil
	}

	key := cache.Key("catalog", "acct-1a3c", "product", "9c1e")
	for range 3 {
		p, err := cache.GetOrSet(ctx, c, key, time.Minute, load)
		if err != nil {
			panic(err)
		}
		_ = p
	}
	fmt.Println("loads:", loads)
	// Output: loads: 1
}

// A failure is never cached. Caching one means a transient outage outlives
// the incident that caused it.
func ExampleGetOrSet_doesNotCacheFailures() {
	c := cache.NewMemory(time.Minute)
	defer c.Close() //nolint:errcheck

	loads := 0
	load := func(context.Context) (product, error) {
		loads++
		return product{}, errors.New("upstream down")
	}

	for range 3 {
		_, _ = cache.GetOrSet(context.Background(), c, "k", time.Minute, load)
	}
	fmt.Println("loads:", loads)
	// Output: loads: 3
}

// Always namespace a key, and include the account where the data is
// per-account. A key without it is one customer's data served to another.
func ExampleKey() {
	fmt.Println(cache.Key("catalog", "acct-1a3c", "product", "9c1e"))
	// Output: catalog:acct-1a3c:product:9c1e
}
