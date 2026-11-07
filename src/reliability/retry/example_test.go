package retry_test

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/IncepTools/go/src/core/errs"
	"github.com/IncepTools/go/src/reliability/retry"
)

func fast() retry.Policy {
	return retry.Policy{
		MaxAttempts: 3,
		BaseDelay:   time.Millisecond,
		MaxDelay:    2 * time.Millisecond,
		NoJitter:    true, // deterministic, for this example only
	}
}

// An unavailable dependency is worth another attempt.
func Example() {
	attempts := 0
	quote, err := retry.Value(context.Background(), fast(),
		func(context.Context) (string, error) {
			attempts++
			if attempts < 3 {
				return "", errs.Unavailable("the pricing service is down")
			}
			return "£12.00", nil
		})

	fmt.Println(quote, err, attempts)
	// Output: £12.00 <nil> 3
}

// A client error is not retried. Three requests that were always going to
// fail help nobody.
func ExampleDo_doesNotRetryAClientError() {
	attempts := 0
	_ = retry.Do(context.Background(), fast(), func(context.Context) error {
		attempts++
		return errs.Invalid("price must be 0 or more")
	})

	fmt.Println("attempts:", attempts)
	// Output: attempts: 1
}

// Neither is an error nobody classified: if nothing has decided what it
// means, it must not be assumed safe to repeat.
func ExampleRetryable() {
	fmt.Println(retry.Retryable(errs.Unavailable("down")))
	fmt.Println(retry.Retryable(errs.Timeout("slow")))
	fmt.Println(retry.Retryable(errs.Conflict("raced").WithCode("CONCURRENT_MODIFICATION")))
	fmt.Println(retry.Retryable(errs.Conflict("taken").WithCode("SKU_TAKEN")))
	fmt.Println(retry.Retryable(errors.New("something happened")))
	// Output:
	// true
	// true
	// true
	// false
	// false
}
