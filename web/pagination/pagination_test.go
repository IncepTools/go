package pagination_test

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/IncepTools/go/core/errs"
	"github.com/IncepTools/go/web/pagination"
)

// parse drives FromWith through a real request, since that is how it is used.
func parse(t *testing.T, query string, opt pagination.Options) (pagination.Query, error) {
	t.Helper()
	var (
		got pagination.Query
		err error
	)
	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error {
		got, err = pagination.FromWith(c, opt)
		return c.SendStatus(200)
	})
	req := httptest.NewRequest("GET", "/?"+query, nil)
	if _, terr := app.Test(req); terr != nil {
		t.Fatal(terr)
	}
	return got, err
}

func TestDefaults(t *testing.T) {
	q, err := parse(t, "", pagination.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if q.Page != 1 || q.Limit != pagination.DefaultLimit {
		t.Fatalf("q = %+v", q)
	}
	if q.Offset() != 0 {
		t.Fatalf("offset = %d", q.Offset())
	}
}

func TestLimitIsClampedNotRejected(t *testing.T) {
	q, err := parse(t, "limit=100000", pagination.Options{MaxLimit: 50})
	if err != nil {
		t.Fatalf("an over-large limit should be clamped, not refused: %v", err)
	}
	if q.Limit != 50 {
		t.Fatalf("limit = %d, want 50", q.Limit)
	}
}

func TestSortIsRefusedWhenNotAllowListed(t *testing.T) {
	_, err := parse(t, "sort=password", pagination.Options{Sortable: []string{"created_at"}})
	if err == nil {
		t.Fatal("an arbitrary sort key was accepted; it is interpolated into ORDER BY")
	}
	if !errs.Is(err, errs.KindInvalid) {
		t.Fatalf("err kind = %q", errs.KindOf(err))
	}
}

func TestSortingFailsClosedWithNoAllowList(t *testing.T) {
	_, err := parse(t, "sort=created_at", pagination.Options{})
	if err == nil {
		t.Fatal("sorting was allowed with no allow-list configured")
	}
}

func TestOffsetAndOrderBy(t *testing.T) {
	q, err := parse(t, "page=3&limit=10&sort=name&order=desc", pagination.Options{Sortable: []string{"name"}})
	if err != nil {
		t.Fatal(err)
	}
	if q.Offset() != 20 {
		t.Fatalf("offset = %d, want 20", q.Offset())
	}
	if q.OrderBy() != "name DESC" {
		t.Fatalf("orderBy = %q", q.OrderBy())
	}
}

func TestBadPageIsAClientError(t *testing.T) {
	for _, bad := range []string{"page=0", "page=-1", "page=abc", "limit=0", "order=sideways"} {
		if _, err := parse(t, bad, pagination.Options{}); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

func TestPages(t *testing.T) {
	cases := []struct{ total, limit, want int }{
		{0, 10, 0}, {1, 10, 1}, {10, 10, 1}, {11, 10, 2}, {25, 10, 3},
	}
	for _, c := range cases {
		if got := pagination.Pages(int64(c.total), c.limit); got != c.want {
			t.Errorf("Pages(%d,%d) = %d, want %d", c.total, c.limit, got, c.want)
		}
	}
}
