package health_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/IncepTools/go/src/web/health"
)

// Severity is the decision that matters. An optional dependency degrades
// the service; it does not take it out of rotation.
func Example() {
	r := health.New("shop", "1.4.2")
	r.Register(
		health.Of("database", health.Critical, func(context.Context) error { return nil }),
		health.Of("cache", health.Optional, func(context.Context) error {
			return errors.New("connection refused")
		}),
	)

	report := r.Run(context.Background())
	fmt.Println("overall:", report.Status)
	fmt.Println("database:", report.Checks["database"].Status)
	fmt.Println("cache:", report.Checks["cache"].Status)
	// Output:
	// overall: DEGRADED
	// database: UP
	// cache: DEGRADED
}

// A critical dependency failing is what takes the service out of rotation,
// and only that.
func ExampleRegistry_Run_criticalFailure() {
	r := health.New("shop", "1.4.2")
	r.Register(health.Of("database", health.Critical,
		func(context.Context) error { return errors.New("connection refused") }))

	fmt.Println(r.Run(context.Background()).Status)
	// Output: DOWN
}
