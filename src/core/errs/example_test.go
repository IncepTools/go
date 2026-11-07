package errs_test

import (
	"errors"
	"fmt"

	"github.com/IncepTools/go/src/core/errs"
)

// A service decides what a failure means without importing net/http. The
// transport reads the Kind and picks a status.
func Example() {
	err := errs.Conflict("a product with the SKU %q already exists", "DESK-01").
		WithCode("SKU_TAKEN")

	fmt.Println(errs.KindOf(err))
	fmt.Println(errs.CodeOf(err))
	fmt.Println(err)
	// Output:
	// conflict
	// SKU_TAKEN
	// a product with the SKU "DESK-01" already exists
}

// The cause is for the log; the message is for the client. Everything in
// WithCause and WithMeta stays out of the response.
func ExampleError_WithCause() {
	driver := errors.New("pq: SSL is not enabled on the server (host=db-7.internal)")

	err := errs.Unavailable("the catalog is temporarily unavailable").
		WithCode("CATALOG_UNAVAILABLE").
		WithCause(driver).
		WithMeta("host", "db-7.internal")

	fmt.Println(err.Message)            // sent to the client
	fmt.Println(errors.Is(err, driver)) // still reachable for the log
	// Output:
	// the catalog is temporarily unavailable
	// true
}

// An error nobody classified is treated as internal, so its text cannot
// reach a client by accident.
func ExampleFrom() {
	plain := errors.New("dial tcp 10.1.4.7:5432: connection refused")

	e := errs.From(plain)
	fmt.Println(errs.KindOf(e))
	fmt.Println(e.Message)
	// Output:
	// internal
	// internal error
}

// The chain survives fmt.Errorf wrapping, so a caller several layers up can
// still branch on the kind.
func ExampleIs() {
	inner := errs.NotFound("product not found").WithCode("NOT_FOUND")
	outer := fmt.Errorf("loading the product page: %w", inner)

	fmt.Println(errs.Is(outer, errs.KindNotFound))
	fmt.Println(errs.CodeOf(outer))
	// Output:
	// true
	// NOT_FOUND
}

// Field-scoped details are what a form attaches to an input.
func ExampleError_WithDetails() {
	err := errs.Invalid("validation failed").
		WithCode("VALIDATION_FAILED").
		WithDetails(
			errs.Detail{Field: "name", Message: "name is required", Rule: "required"},
			errs.Detail{Field: "price", Message: "price must be 0 or more", Rule: "gte"},
		)

	for _, d := range err.Details {
		fmt.Printf("%s: %s\n", d.Field, d.Message)
	}
	// Output:
	// name: name is required
	// price: price must be 0 or more
}
