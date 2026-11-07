# Pagination

`web/pagination` reads list parameters off a request, with the bounds applied
before anything reaches the database.

```go
q, err := pagination.FromWith(c, pagination.Options{
	Sortable:     constants.Sortable,
	DefaultSort:  "created_at",
	DefaultOrder: pagination.Desc,
})
if err != nil {
	return response.Fail(c, err)
}

page, err := c.svc.List(ctx.Context(), q)
if err != nil {
	return response.Fail(c, err)
}
return response.List(c, constants.MsgListed, items, q.Page, q.Limit, page.Total)
```

```
GET /api/v1/catalog/products?page=2&limit=50&sort=name&order=asc&search=lamp
```

```json
"meta": { "page": 2, "pageSize": 50, "total": 134, "pages": 3 }
```

## The two things it is for

**Bounds.** `?limit=1000000` against a table with ten million rows is not a
client being greedy, it is an outage. Clamping at the edge means no repository
has to remember to.

An over-large limit is **clamped, not rejected**. A client asking for more
than the ceiling gets the ceiling and a full page of data; rejecting it turns
a working integration into a 400 the day you lower the ceiling.

**An allow-listed sort.** `Query.OrderBy()` is interpolated into SQL. It is
safe to interpolate *only* because the sort key was checked against a list
your code owns:

```go
var Sortable = []string{"created_at", "name", "price"}
```

```
GET /products?sort=password
→ 400 INVALID_SORT  "sort must be one of: created_at, name, price"
```

With no `Sortable` configured, sorting is refused entirely. Failing closed is
the only safe default, because the alternative is interpolating request input
into a query.

**Never** build an `ORDER BY` from a raw query parameter. That is a SQL
injection with extra steps.

## Defaults

| | |
|---|---|
| `DefaultPage` | 1 |
| `DefaultLimit` | 20 |
| `MaxLimit` | 100 |

Override per route:

```go
pagination.Options{DefaultLimit: 50, MaxLimit: 500}
```

Raise `MaxLimit` only where the rows are small and the query is indexed. The
ceiling is the thing standing between a client's loop and your database.

## The query value

```go
type Query struct {
	Page   int
	Limit  int
	Sort   string
	Order  Direction  // Asc | Desc
	Search string     // trimmed; empty means no filter
}

q.Offset()    // (Page-1) * Limit
q.OrderBy()   // "name ASC", or "" when no sort was requested
```

In a repository, `gormx.Paginate(q)` applies all three:

```go
page, err := r.List(ctx, q, gormx.Where("category_id = ?", id))
```

## Search

`Query.Search` is the trimmed free-text term, and nothing more — the package
does not decide how you search. Use it deliberately:

```go
if q.Search != "" {
	scopes = append(scopes, gormx.Where("name ILIKE ?", "%"+q.Search+"%"))
}
```

A leading-wildcard `LIKE` cannot use a B-tree index. It is fine on a small
table and a problem on a large one; reach for a trigram index or a real
search index when it stops being fine.

## Errors

| Input | |
|---|---|
| `page=0`, `page=-1`, `page=abc` | 400 `INVALID_PAGINATION` |
| `limit=0`, `limit=abc` | 400 `INVALID_PAGINATION` |
| `limit=100000` | clamped |
| `sort=whatever` | 400 `INVALID_SORT` |
| `order=sideways` | 400 `INVALID_SORT` |

Each carries a field-scoped detail, so a form can attach it to the input.

## Offset paging and its limit

This is offset paging, and offset paging degrades: `OFFSET 100000` makes the
database walk a hundred thousand rows to discard them. It is also unstable —
a row inserted between two requests shifts everything and the client sees a
duplicate or a gap.

That is an acceptable trade for a console listing tens of pages, and the
wrong one for an export or a sync. For those, give the caller a keyset:

```go
// WHERE (created_at, id) < (?, ?) ORDER BY created_at DESC, id DESC LIMIT ?
```

A cursor API belongs in the module that needs it, not in every list endpoint.

## Next

- [Database](database.md) — `List`, scopes, and why COUNT is a second query.
- [Controllers](controllers.md) — where the parsed query is used.
