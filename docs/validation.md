# Validation

`web/binding` turns a request into a validated DTO, or into an error that
already knows how to render itself.

```go
req, err := binding.Body[dto.CreateProduct](c)
if err != nil {
	return response.Fail(c, err)
}
```

```json
{
  "meta": {
    "statusCode": 400,
    "code": "VALIDATION_FAILED",
    "message": "validation failed",
    "errors": [
      { "field": "name",     "message": "name must be at least 2 characters", "rule": "min" },
      { "field": "tags[0]",  "message": "tags[0] is required",                "rule": "required" }
    ]
  }
}
```

Field names are the **JSON** names the client sent, not the Go field names. A
client that sent `category_id` is told `category_id is required`, not
`CategoryID is required`.

## Binders

| | |
|---|---|
| `binding.Body[T](c)` | JSON request body |
| `binding.Query[T](c)` | Query string |
| `binding.Params[T](c)` | Path parameters |
| `binding.Header[T](c)` | Request headers |
| `binding.Into(c, &dst)` | When the type is not known statically |
| `binding.Validate(v)` | Validate something you already have |

## Writing a DTO

```go
type CreateProduct struct {
	Name        string   `json:"name" validate:"required,min=2,max=120"`
	SKU         string   `json:"sku" validate:"required,alphanum,len=8"`
	Price       int      `json:"price" validate:"gte=0"`
	Currency    string   `json:"currency" validate:"required,oneof=USD EUR GBP"`
	CategoryID  string   `json:"category_id" validate:"required,uuid"`
	Tags        []string `json:"tags" validate:"omitempty,max=10,dive,required,max=24"`
}
```

### Updates use pointers

```go
type UpdateProduct struct {
	Name     *string `json:"name" validate:"omitempty,min=2,max=120"`
	Price    *int    `json:"price" validate:"omitempty,gte=0"`
	Archived *bool   `json:"archived"`
}
```

This is not a style preference. With a plain `bool` there is no way to tell
"the client did not send this field" from "the client sent `false`" — so a
request that only renames cannot be distinguished from one asking to
un-archive, and a request to set a flag to `false` does nothing at all.

The service then applies exactly what was sent:

```go
fields := map[string]any{}
if req.Name != nil {
	fields["name"] = *req.Name
}
if req.Archived != nil {
	fields["archived"] = *req.Archived
}
```

A map, not a struct, for the same reason on the way out — see
[Database](database.md).

### Responses are separate types

Returning the entity publishes every column you ever add to it, including the
internal ones, and turns a schema change into an unannounced API change.

## The three hooks

A DTO can implement any of these. They run in this order:

```go
// 1. Normalize — clean the input, before the rules see it.
func (c *CreateProduct) Normalize() {
	c.Name = strings.TrimSpace(c.Name)
	c.SKU = strings.ToUpper(strings.TrimSpace(c.SKU))
}

// 2. SetDefaults — fill optional fields, so a default can satisfy `required`.
func (c *CreateProduct) SetDefaults() {
	if c.Currency == "" {
		c.Currency = "USD"
	}
}

// 3. Validate — the rule a tag cannot express. Runs AFTER the tag rules,
//    so the individual fields are already well-formed.
func (c *CreateProduct) Validate() error {
	if c.SalePrice != nil && *c.SalePrice > c.Price {
		return errs.Invalid("the sale price is above the list price").
			WithDetails(errs.Detail{
				Field:   "sale_price",
				Message: "sale_price must not exceed price",
				Rule:    "lte_price",
			})
	}
	return nil
}
```

Normalising **before** validation matters: the rules should see the value
that will actually be stored, not the one that arrived.

## Messages

Built-in rules render readable sentences, in the unit the rule actually uses:

| Rule | Message |
|---|---|
| `required` | `name is required` |
| `min=2` on a string | `name must be at least 2 characters` |
| `min=1` on a slice | `tags must be at least 1 entries` |
| `oneof=USD EUR` | `currency must be one of: USD, EUR` |
| `uuid` | `category_id must be a valid UUID` |
| `email`, `url`, `ip`, `cidr`, `hostname` | the obvious thing |

"name must be at least 2" is a worse message than "at least 2 characters",
and "tags must be at least 1 characters" is simply wrong — hence the unit.

## Custom rules

```go
func init() {
	_ = binding.RegisterRule("sku", func(fl validator.FieldLevel) bool {
		return skuPattern.MatchString(fl.Field().String())
	})
	binding.RegisterMessage("sku", func(fe validator.FieldError) string {
		return "sku must be 3 letters followed by 5 digits"
	})
}
```

Always register a message. Without one a custom rule renders as
`<field> is invalid`, which tells a user nothing about how to fix it.

For struct-level rules, reach for the underlying instance:

```go
binding.Validator().RegisterStructValidation(checkDateRange, DateRange{})
```

## Validating outside a request

```go
if details := binding.Validate(&cmd); len(details) > 0 {
	return errs.Invalid("validation failed").WithDetails(details...)
}
```

Useful in a CLI or a queue consumer, where the same DTO arrives by another
route.

## Conventions

**Use `required` and mean it.** A field with no `validate` tag is accepted
however it arrives — including absent.

**Bound every string and every slice.** `max=120` on a name and `max=10` on a
list are the difference between a validation error and a 4MB row.

**Validate the shape, not the business rule.** "is a UUID" is validation;
"this category belongs to you" is the service's job, because it needs the
database and because a job needs the same check.

## Next

- [Pipes](pipes.md) — single parameters rather than whole DTOs.
- [Exception filters](exception-filters.md) — how a failure is rendered.
