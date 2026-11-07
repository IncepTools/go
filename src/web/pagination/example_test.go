package pagination_test

import (
	"fmt"
	"net/http/httptest"

	"github.com/gofiber/fiber/v3"

	"github.com/IncepTools/go/src/core/errs"
	"github.com/IncepTools/go/src/web/pagination"
)

// serve runs one request through a handler and returns what it saw.
func serve(query string, opt pagination.Options) (pagination.Query, error) {
	var (
		got pagination.Query
		err error
	)
	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error {
		got, err = pagination.FromWith(c, opt)
		return c.SendStatus(200)
	})
	//nolint:errcheck // the handler above records what matters
	app.Test(httptest.NewRequest("GET", "/?"+query, nil))
	return got, err
}

// Sorting is restricted to an allow-list the caller owns, because
// OrderBy is interpolated into SQL.
func Example() {
	q, err := serve("page=3&limit=10&sort=name&order=desc", pagination.Options{
		Sortable: []string{"created_at", "name"},
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(q.Offset())
	fmt.Println(q.OrderBy())
	// Output:
	// 20
	// name DESC
}

// An over-large limit is clamped rather than refused. Rejecting it would
// turn a working integration into a 400 the day the ceiling is lowered.
func ExampleFromWith_clampsTheLimit() {
	q, err := serve("limit=100000", pagination.Options{MaxLimit: 50})
	fmt.Println(q.Limit, err)
	// Output: 50 <nil>
}

// A sort key that is not allow-listed is refused, with a message naming
// what is.
func ExampleFromWith_refusesAnUnknownSortKey() {
	_, err := serve("sort=password", pagination.Options{
		Sortable: []string{"created_at", "name"},
	})

	fmt.Println(errs.CodeOf(err))
	fmt.Println(errs.From(err).Details[0].Message)
	// Output:
	// INVALID_SORT
	// sort must be one of: created_at, name
}
