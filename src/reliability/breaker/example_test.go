package breaker_test

import (
	"context"
	"fmt"
	"time"

	"github.com/IncepTools/go/src/core/errs"
	"github.com/IncepTools/go/src/reliability/breaker"
)

// After enough consecutive failures the breaker opens and stops calling
// the dependency at all, which frees the workers it was blocking.
func Example() {
	b := breaker.New(breaker.Options{
		Name: "pricing-api", Threshold: 3, Cooldown: time.Minute,
	})
	ctx := context.Background()

	for range 3 {
		_ = b.Do(ctx, func(context.Context) error {
			return errs.Unavailable("connection refused")
		})
	}
	fmt.Println("state:", b.State())

	called := false
	err := b.Do(ctx, func(context.Context) error { called = true; return nil })

	fmt.Println("dependency called:", called)
	fmt.Println("code:", errs.CodeOf(err))
	// Output:
	// state: open
	// dependency called: false
	// code: CIRCUIT_OPEN
}

// A 404 from a dependency is a perfectly healthy answer. Counting it would
// mean a client asking for missing things repeatedly takes the dependency
// out for everyone.
func ExampleNew_clientErrorsDoNotCount() {
	b := breaker.New(breaker.Options{Threshold: 2, Cooldown: time.Minute})

	for range 10 {
		_ = b.Do(context.Background(), func(context.Context) error {
			return errs.NotFound("no price for that SKU")
		})
	}
	fmt.Println(b.State())
	// Output: closed
}
