# Pipes

`web/pipe` reads one value out of a request and converts it, returning an
error that already carries the right status and names the right field.

```go
id, err := pipe.UUIDParam(c, "id")
if err != nil {
	return response.Fail(c, err)
}
```

```json
{
  "meta": {
    "statusCode": 400,
    "code": "INVALID_PARAMETER",
    "message": "id must be a valid UUID",
    "errors": [{ "field": "id", "message": "id must be a valid UUID", "rule": "uuid" }]
  }
}
```

It exists because the alternative — `uuid.Parse(c.Params("id"))` with a
hand-written error beside it — is written slightly differently at every call
site, and some of those forget the error entirely and pass a zero UUID into a
query.

## Available

| | |
|---|---|
| `pipe.UUIDParam(c, "id")` | Required path parameter as a UUID. |
| `pipe.UUIDQuery(c, "category_id")` | **Optional** query parameter as a UUID. |
| `pipe.StringParam(c, "slug")` | Required, trimmed. |
| `pipe.IntParam(c, "page")` | Required integer. |
| `pipe.IntQuery(c, "depth", 1)` | Optional integer with a default. |
| `pipe.BoolQuery(c, "archived", false)` | Accepts true/false, 1/0, yes/no, on/off. |
| `pipe.EnumQuery(c, "status", "open", "closed")` | Constrained to a fixed set. |
| `pipe.TimeQuery(c, "since")` | Optional RFC 3339 timestamp. |
| `pipe.CSVQuery(c, "tags")` | Comma-separated list, trimmed, empties dropped. |

## Required versus optional

The distinction is deliberate and worth noticing.

```go
pipe.UUIDParam(c, "id")          // missing  → error
pipe.UUIDQuery(c, "category_id") // missing  → uuid.Nil, nil
                                 // garbage  → error
```

`?category_id=` meaning "every category" and `?category_id=oops` meaning "a
typo" must not behave the same way. The first is a filter the caller chose not
to apply; the second is a mistake they want to hear about.

`EnumQuery` returns `""` for a missing value rather than guessing, so the
caller decides the default in one visible place.

## In a handler

```go
func (c *ProductController) list(ctx fiber.Ctx) error {
	q, err := pagination.FromWith(ctx, pagination.Options{
		Sortable: constants.Sortable, DefaultSort: "created_at",
	})
	if err != nil {
		return response.Fail(ctx, err)
	}
	categoryID, err := pipe.UUIDQuery(ctx, "category_id")
	if err != nil {
		return response.Fail(ctx, err)
	}
	archived, err := pipe.BoolQuery(ctx, "archived", false)
	if err != nil {
		return response.Fail(ctx, err)
	}

	page, err := c.svc.List(ctx.Context(), q, categoryID, archived)
	...
}
```

## Structured query strings

Several related parameters are better bound as a struct, with validation
rules, than pulled out one at a time:

```go
type ListFilter struct {
	CategoryID string `query:"category_id" validate:"omitempty,uuid"`
	Status     string `query:"status" validate:"omitempty,oneof=draft live archived"`
	MinPrice   int    `query:"min_price" validate:"omitempty,gte=0"`
}

filter, err := binding.Query[ListFilter](c)
```

See [Validation](validation.md).

## Writing your own

Return an `*errs.Error` with a `Detail` naming the field, so the response is
the same shape as every other parameter failure:

```go
func CurrencyQuery(c fiber.Ctx, name string) (string, error) {
	raw := strings.ToUpper(strings.TrimSpace(c.Query(name)))
	if raw == "" {
		return "", nil
	}
	if !currency.IsValid(raw) {
		return "", errs.Invalid("%s is not a known currency", name).
			WithCode("INVALID_PARAMETER").
			WithDetails(errs.Detail{
				Field:   name,
				Message: name + " must be an ISO 4217 currency code",
				Rule:    "currency",
			})
	}
	return raw, nil
}
```

## Next

- [Validation](validation.md) — binding whole DTOs.
- [Pagination](pagination.md) — the list parameters, bounded.
