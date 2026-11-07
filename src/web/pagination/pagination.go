// Package pagination reads list parameters off a request, with the bounds
// applied before anything reaches the database.
//
// The bound is the point. `?limit=1000000` on a table with ten million rows is
// not a client being greedy, it is an outage; clamping at the edge means no
// repository has to remember to.
package pagination

import (
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/IncepTools/go/src/core/errs"
)

// Defaults applied when the request says nothing.
const (
	DefaultPage  = 1
	DefaultLimit = 20
	MaxLimit     = 100
)

// Direction is a sort order.
type Direction string

const (
	// Asc sorts ascending.
	Asc Direction = "asc"
	// Desc sorts descending.
	Desc Direction = "desc"
)

// Query is the parsed list request.
type Query struct {
	Page  int       `json:"page"`
	Limit int       `json:"limit"`
	Sort  string    `json:"sort,omitempty"`
	Order Direction `json:"order,omitempty"`
	// Search is the free-text term, trimmed. Empty means no filter.
	Search string `json:"search,omitempty"`
}

// Offset is the SQL OFFSET for this page.
func (q Query) Offset() int {
	if q.Page < 1 {
		return 0
	}
	return (q.Page - 1) * q.Limit
}

// OrderBy renders a SQL ORDER BY clause, or "" when no sort was requested.
//
// It is safe to interpolate ONLY because From rejects any sort key that is not
// in the caller's allow-list. Never build one of these from a raw query
// parameter; that is a SQL injection with extra steps.
func (q Query) OrderBy() string {
	if q.Sort == "" {
		return ""
	}
	dir := "ASC"
	if q.Order == Desc {
		dir = "DESC"
	}
	return q.Sort + " " + dir
}

// Options configures parsing.
type Options struct {
	// DefaultLimit overrides DefaultLimit for this route.
	DefaultLimit int
	// MaxLimit overrides MaxLimit for this route.
	MaxLimit int
	// Sortable is the allow-list of column names a client may sort by. When
	// it is empty, sorting is refused entirely — fail closed, because the
	// alternative is interpolating client input into SQL.
	Sortable []string
	// DefaultSort is applied when the client asks for none. It is NOT checked
	// against Sortable; it comes from your code, not the request.
	DefaultSort  string
	DefaultOrder Direction
}

// From parses the query string with the defaults.
func From(c fiber.Ctx) Query {
	q, _ := FromWith(c, Options{})
	return q
}

// FromWith parses the query string and reports a client error for anything it
// refuses — an unsortable column, a non-numeric page.
func FromWith(c fiber.Ctx, opt Options) (Query, error) {
	def := opt.DefaultLimit
	if def <= 0 {
		def = DefaultLimit
	}
	maximum := opt.MaxLimit
	if maximum <= 0 {
		maximum = MaxLimit
	}

	q := Query{Page: DefaultPage, Limit: def, Sort: opt.DefaultSort, Order: opt.DefaultOrder}

	if raw := strings.TrimSpace(c.Query("page")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return q, errs.Invalid("page must be a positive whole number").
				WithCode("INVALID_PAGINATION").
				WithDetails(errs.Detail{Field: "page", Message: "page must be a positive whole number", Rule: "numeric"})
		}
		q.Page = n
	}

	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return q, errs.Invalid("limit must be a positive whole number").
				WithCode("INVALID_PAGINATION").
				WithDetails(errs.Detail{Field: "limit", Message: "limit must be a positive whole number", Rule: "numeric"})
		}
		// Clamp rather than reject. A client asking for more than the ceiling
		// gets the ceiling and a full page of data; rejecting it turns a
		// working integration into a 400 the day the ceiling is lowered.
		q.Limit = min(n, maximum)
	}

	if raw := strings.TrimSpace(c.Query("sort")); raw != "" {
		if !allowed(raw, opt.Sortable) {
			return q, errs.Invalid("cannot sort by %q", raw).
				WithCode("INVALID_SORT").
				WithDetails(errs.Detail{
					Field:   "sort",
					Message: sortMessage(opt.Sortable),
					Rule:    "oneof",
				})
		}
		q.Sort = raw
	}

	switch strings.ToLower(strings.TrimSpace(c.Query("order"))) {
	case "":
	case "asc":
		q.Order = Asc
	case "desc":
		q.Order = Desc
	default:
		return q, errs.Invalid("order must be asc or desc").
			WithCode("INVALID_SORT").
			WithDetails(errs.Detail{Field: "order", Message: "order must be asc or desc", Rule: "oneof"})
	}
	if q.Sort != "" && q.Order == "" {
		q.Order = Asc
	}

	q.Search = strings.TrimSpace(c.Query("search"))
	return q, nil
}

func allowed(key string, list []string) bool {
	for _, k := range list {
		if k == key {
			return true
		}
	}
	return false
}

func sortMessage(sortable []string) string {
	if len(sortable) == 0 {
		return "sorting is not supported on this endpoint"
	}
	return "sort must be one of: " + strings.Join(sortable, ", ")
}

// Pages is the total page count for a result size.
func Pages(total int64, limit int) int {
	if limit <= 0 {
		return 0
	}
	pages := int(total) / limit
	if int(total)%limit != 0 {
		pages++
	}
	return pages
}
