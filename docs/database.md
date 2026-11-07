# Database

`data/repo` is a generic, account-scoped repository over GORM. `data/gormx`
translates driver errors and provides composable query scopes.

## Why a repository rather than GORM directly

One reason above all the convenience: **the ownership filter is applied by the
repository, not by the caller.**

Hand-written scoping means every query in the codebase has to remember
`.Where("account_id = ?", id)`, and the one that forgets does not fail — it
returns another customer's rows, silently, forever, until somebody notices.

Here the repository applies it itself and **refuses** a query with no account
on the context. The unscoped operation still exists, because reports and
reconcilers genuinely need it; it just cannot happen by omission.

## Declaring one

```go
type ProductRepository struct {
	*repo.Repository[entity.Product]
}

func NewProductRepository(db *gorm.DB) *ProductRepository {
	return &ProductRepository{
		Repository: repo.New[entity.Product](db,
			repo.WithEntityName("product"),                  // for error messages
			repo.Tenanted(),                                 // scoped on account_id
			repo.WithSortable("created_at", "name", "price"),
		),
	}
}
```

`WithEntityName` is what makes `product not found` instead of `record not
found`. `WithTenantColumn("org_id")` if your column is named differently;
omit it entirely for a table that is not per-account — a currency table, a
feature flag.

## The surface

```go
r.FindByID(ctx, id, scopes...)      (*T, error)   // NotFound if absent or not yours
r.FindOne(ctx, scopes...)           (*T, error)
r.List(ctx, q, scopes...)           (Page[T], error)
r.All(ctx, scopes...)               ([]T, error)
r.Count(ctx, scopes...)             (int64, error)
r.Exists(ctx, scopes...)            (bool, error)

r.Create(ctx, &model)               error
r.CreateMany(ctx, models)           error
r.Save(ctx, &model)                 error
r.UpdateByID(ctx, id, fields)       error
r.UpdateFields(ctx, fields, scopes...) (int64, error)
r.DeleteByID(ctx, id)               error
r.Delete(ctx, scopes...)            (int64, error)
r.LockByID(ctx, id)                 (*T, error)   // FOR UPDATE, inside a tx
```

## The refusals

These are the parts worth knowing before you need them.

**No account on the context is an error, never "all accounts".**

```go
_, err := r.List(ctx, q)
// errs.Unauthorized, code NO_TENANT
```

**Create takes the account from the context, not the payload.** A caller
cannot insert a row under another account by setting the field.

**`UpdateFields` and `Delete` refuse an empty condition.** An update with no
`WHERE` rewrites every row the account owns.

```go
_, err := r.UpdateFields(ctx, fields)   // UNCONDITIONAL_UPDATE
```

**`UpdateFields` takes a map, not a struct.** GORM skips zero-valued struct
fields, so a struct update can never set a column to `false`, `0` or `""`.
That silent skip is a recurring defect: a PATCH that disables something
appears to succeed and changes nothing.

```go
fields := map[string]any{}
if req.Archived != nil {
	fields["archived"] = *req.Archived   // false is written
}
```

**Another account's row is `NotFound`, not `Forbidden`.** Forbidden would
confirm the id exists to somebody who should not know.

## Going unscoped

```go
func (r *ProductRepository) CountAcrossAllAccounts(ctx context.Context) (int64, error) {
	var n int64
	return n, r.Unscoped(ctx).Model(&entity.Product{}).Count(&n).Error
}
```

Every use is a deliberate crossing of the ownership boundary. Make it obvious
at the call site, give it a name that says so, and keep it out of request
paths.

## Scopes

```go
page, err := r.List(ctx, q,
	gormx.Where("category_id = ?", categoryID),
	gormx.Where("price >= ?", minPrice),
	gormx.Preload("Images"),
)
```

| | |
|---|---|
| `gormx.Where(q, args...)` | A condition. |
| `gormx.Preload(assoc)` | Eager-load an association. |
| `gormx.Order(clause)` | Sort by a column **you** name in code. |
| `gormx.Paginate(q)` | LIMIT, OFFSET and ORDER BY from a page request. |
| `gormx.ForUpdate()` | Row lock for the rest of the transaction. |

Reach for `Preload` whenever a list handler reads an association per row. The
N+1 is the most common performance defect in this kind of service, and it
never shows up in development, where the list has four rows.

`gormx.Order` is for a clause from your code. Never hand it request input —
see [Pagination](pagination.md) for the allow-list that makes a client's sort
key safe.

## Paging

```go
q, err := pagination.FromWith(c, pagination.Options{
	Sortable: constants.Sortable, DefaultSort: "created_at", DefaultOrder: pagination.Desc,
})
page, err := r.List(ctx, q)
// page.Items []T, page.Total int64
```

`List` runs the COUNT and the SELECT as two queries. That is two round trips
rather than one window function — a deliberate trade: the window form breaks
the moment a scope adds a JOIN that multiplies rows, and a wrong total is
worse than a second query.

An empty result returns `[]T{}`, not `nil`. A JSON `"items": null` is a
different thing to a client than `"items": []`.

## Transactions

```go
err := repo.Transaction(ctx, db, func(ctx context.Context) error {
	if err := products.Create(ctx, p); err != nil {
		return err
	}
	if err := inventory.Reserve(ctx, p.ID, 10); err != nil {
		return err   // both roll back
	}
	return outbox.Enqueue(ctx, db, "product.created", p)
})
```

The transaction travels on the **context**, so every repository call inside
joins it automatically. No repository takes a `*gorm.DB` parameter and none of
them knows a transaction exists. That is what makes them composable.

Nesting **joins** the outer transaction rather than opening a savepoint. That
is what a service wants: when a step inside a larger operation fails,
everything rolls back together, where a savepoint would let the outer work
commit without it.

Inject a `*repo.TxManager` where a service should be able to run a
transaction without depending on `*gorm.DB`:

```go
func (s *Service) Publish(ctx context.Context, id uuid.UUID) error {
	return s.tx.Do(ctx, func(ctx context.Context) error { ... })
}
```

## Row locks

```go
err := repo.Transaction(ctx, db, func(ctx context.Context) error {
	item, err := r.LockByID(ctx, id)     // SELECT ... FOR UPDATE
	if err != nil {
		return err
	}
	item.Stock -= qty
	return r.Save(ctx, item)
})
```

Use it for read-modify-write on a counter, a quota or an allocation. The
alternative — read, decide, write — is a lost update under any concurrency at
all, and the window is wide enough to hit in practice.

`LockByID` refuses outside a transaction, because a row lock outside one is
released immediately and protects nothing.

## Error translation

Driver errors become typed errors with the original kept as the cause:

| Database | Becomes | Code |
|---|---|---|
| no rows | `NotFound` | `NOT_FOUND` |
| unique violation (23505) | `Conflict` | `ALREADY_EXISTS` |
| foreign key (23503) | `Conflict` | `STILL_REFERENCED` |
| not-null (23502) | `Invalid` | `MISSING_FIELD` |
| check (23514) | `Invalid` | `CONSTRAINT_VIOLATED` |
| serialization / deadlock (40001, 40P01) | `Conflict` | `CONCURRENT_MODIFICATION` |
| anything else | `Internal` | — |

Two failure modes this prevents, both real: a duplicate name rendered as a
500 when the user could have fixed it, and the driver's text — containing the
constraint name, the table and sometimes the value — sent to the client.

`CONCURRENT_MODIFICATION` is the one conflict that is retryable, and
[`retry`](resilience.md) knows it.

## Check before you insert, *and* constrain

```go
exists, err := r.Exists(ctx, gormx.Where("sku = ?", req.SKU))
if exists {
	return errs.Conflict("a product with SKU %q already exists", req.SKU).
		WithCode("SKU_TAKEN")
}
```

Keep the unique index as well. The check alone is a race — two concurrent
creates both see nothing and both insert. The index alone gives the user a
constraint violation instead of a sentence. You need both: the check for the
message, the index for the guarantee.

## Schema

Nothing calls `AutoMigrate`. The schema comes from versioned SQL — see
[Migrations](migrations.md). `module.WithEntities(...)` records ownership,
not structure.

## Next

- [Pagination](pagination.md) · [Migrations](migrations.md)
- [Transactional outbox](outbox.md) — side effects inside the transaction.
