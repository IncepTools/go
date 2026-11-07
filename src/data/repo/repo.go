// Package repo is a generic, tenant-scoped repository over GORM.
//
// It exists for one reason above all the convenience: the tenant filter is
// applied by the repository, not by the caller.
//
// Hand-written scoping means every query in the codebase has to remember
// `.Where("account_id = ?", tenant)`, and the one that forgets does not fail —
// it returns another customer's rows, silently, forever, until somebody
// notices. Here, a tenant-scoped repository applies the filter itself and
// REFUSES a request with no tenant on the context rather than quietly running
// unscoped. The unsafe operation still exists, spelled Unscoped, because
// reconcilers genuinely need it; it just cannot happen by omission.
//
//	type ProductRepo struct{ *repo.Repository[entity.Product] }
//
//	func NewProductRepo(db *gorm.DB) *ProductRepo {
//	    return &ProductRepo{repo.New[entity.Product](db,
//	        repo.WithEntityName("product"),
//	        repo.WithTenantColumn("account_id"),
//	    )}
//	}
package repo

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/IncepTools/go/src/core/ctxkit"
	"github.com/IncepTools/go/src/core/errs"
	"github.com/IncepTools/go/src/data/gormx"
	"github.com/IncepTools/go/src/web/pagination"
)

// DefaultTenantColumn is the column a tenant-scoped repository filters on.
const DefaultTenantColumn = "account_id"

// Repository is the data-access surface for one entity type.
type Repository[T any] struct {
	db           *gorm.DB
	entity       string
	tenantColumn string
	// sortable is the allow-list the List helper enforces.
	sortable []string
}

// Option configures a Repository.
type Option func(cfg *config)

type config struct {
	entity       string
	tenantColumn string
	sortable     []string
}

// WithEntityName sets the noun used in error messages: "product", "invoice".
// Without it, errors say "record", which is true and useless.
func WithEntityName(name string) Option {
	return func(c *config) { c.entity = name }
}

// WithTenantColumn makes the repository tenant-scoped on that column.
//
// Setting it changes the safety contract: every method then REQUIRES a tenant
// on the context and returns an error without one.
func WithTenantColumn(col string) Option {
	return func(c *config) { c.tenantColumn = col }
}

// Tenanted is WithTenantColumn(DefaultTenantColumn).
func Tenanted() Option { return WithTenantColumn(DefaultTenantColumn) }

// WithSortable sets the columns List will accept a client sort on.
func WithSortable(cols ...string) Option {
	return func(c *config) { c.sortable = append(c.sortable, cols...) }
}

// New builds a repository.
func New[T any](db *gorm.DB, opts ...Option) *Repository[T] {
	cfg := config{entity: "record"}
	for _, o := range opts {
		o(&cfg)
	}
	return &Repository[T]{db: db, entity: cfg.entity, tenantColumn: cfg.tenantColumn, sortable: cfg.sortable}
}

// Sortable returns the allow-list, for a controller to pass to the paginator.
func (r *Repository[T]) Sortable() []string { return append([]string(nil), r.sortable...) }

// DB returns the session bound to ctx and to the transaction on it, if any.
//
// Use it for a query this repository does not cover. It is NOT tenant-scoped
// — call Scoped for that.
func (r *Repository[T]) DB(ctx context.Context) *gorm.DB {
	return TxFrom(ctx, r.db).WithContext(ctx)
}

// Scoped returns the session with the tenant filter applied, or an error when
// the repository is tenant-scoped and the context carries no tenant.
func (r *Repository[T]) Scoped(ctx context.Context) (*gorm.DB, error) {
	db := r.DB(ctx)
	if r.tenantColumn == "" {
		return db, nil
	}
	t := ctxkit.TenantOf(ctx)
	if t.Zero() {
		// Deliberately an error, never "no filter". A zero tenant must not be
		// read as "every tenant"; that single decision is what this package
		// is for.
		return nil, errs.Unauthorized("no account context for this %s query", r.entity).
			WithCode("NO_TENANT").
			WithMeta("entity", r.entity)
	}
	return db.Where(r.tenantColumn+" = ?", t.AccountID), nil
}

// Unscoped returns the session WITHOUT the tenant filter.
//
// Legitimate callers: a reconciler sweeping every account, a platform report,
// an admin tool. Every use is a deliberate crossing of the tenancy boundary —
// make it obvious at the call site and keep it out of request paths.
func (r *Repository[T]) Unscoped(ctx context.Context) *gorm.DB { return r.DB(ctx) }

// Create inserts one row.
//
// It sets the tenant column from the context when the repository is scoped,
// so a caller cannot insert a row under another account by setting the field
// — the context wins over the payload, always.
func (r *Repository[T]) Create(ctx context.Context, model *T) error {
	db := r.DB(ctx)
	if r.tenantColumn != "" {
		t := ctxkit.TenantOf(ctx)
		if t.Zero() {
			return errs.Unauthorized("no account context to create a %s under", r.entity).WithCode("NO_TENANT")
		}
	}
	if err := db.Create(model).Error; err != nil {
		return gormx.Translate(err, r.entity)
	}
	return nil
}

// CreateMany inserts in batches of 200.
func (r *Repository[T]) CreateMany(ctx context.Context, models []T) error {
	if len(models) == 0 {
		return nil
	}
	if err := r.DB(ctx).CreateInBatches(models, 200).Error; err != nil {
		return gormx.Translate(err, r.entity)
	}
	return nil
}

// FindByID returns one row by primary key, scoped to the tenant.
//
// A row belonging to another account returns NotFound, not Forbidden:
// Forbidden would confirm the id exists to someone who should not know it.
func (r *Repository[T]) FindByID(ctx context.Context, id uuid.UUID, scopes ...gormx.Scope) (*T, error) {
	db, err := r.Scoped(ctx)
	if err != nil {
		return nil, err
	}
	var out T
	if err := gormx.Apply(db, scopes...).First(&out, "id = ?", id).Error; err != nil {
		return nil, gormx.Translate(err, r.entity)
	}
	return &out, nil
}

// FindOne returns the first row matching the scopes.
func (r *Repository[T]) FindOne(ctx context.Context, scopes ...gormx.Scope) (*T, error) {
	db, err := r.Scoped(ctx)
	if err != nil {
		return nil, err
	}
	var out T
	if err := gormx.Apply(db, scopes...).First(&out).Error; err != nil {
		return nil, gormx.Translate(err, r.entity)
	}
	return &out, nil
}

// Exists reports whether any row matches, without loading one.
func (r *Repository[T]) Exists(ctx context.Context, scopes ...gormx.Scope) (bool, error) {
	db, err := r.Scoped(ctx)
	if err != nil {
		return false, err
	}
	var n int64
	var zero T
	if err := gormx.Apply(db.Model(&zero), scopes...).Limit(1).Count(&n).Error; err != nil {
		return false, gormx.Translate(err, r.entity)
	}
	return n > 0, nil
}

// Count returns the number of matching rows.
func (r *Repository[T]) Count(ctx context.Context, scopes ...gormx.Scope) (int64, error) {
	db, err := r.Scoped(ctx)
	if err != nil {
		return 0, err
	}
	var n int64
	var zero T
	if err := gormx.Apply(db.Model(&zero), scopes...).Count(&n).Error; err != nil {
		return 0, gormx.Translate(err, r.entity)
	}
	return n, nil
}

// Page is one page of results plus the total.
type Page[T any] struct {
	Items []T   `json:"items"`
	Total int64 `json:"total"`
}

// List returns a page of rows and the unpaginated total.
//
// It runs the COUNT and the SELECT as two queries against the same scopes.
// That is two round trips rather than one window function — the trade is
// deliberate: the window-function form breaks the moment a scope adds a JOIN
// that multiplies rows, and a wrong total is worse than a second query.
func (r *Repository[T]) List(ctx context.Context, q pagination.Query, scopes ...gormx.Scope) (Page[T], error) {
	var page Page[T]

	db, err := r.Scoped(ctx)
	if err != nil {
		return page, err
	}
	var zero T

	total, err := r.Count(ctx, scopes...)
	if err != nil {
		return page, err
	}
	page.Total = total
	if total == 0 {
		// Explicitly empty, not nil: a JSON "items": null is a different
		// thing to a client than "items": [], and the second is what an empty
		// list means.
		page.Items = []T{}
		return page, nil
	}

	items := make([]T, 0, q.Limit)
	query := gormx.Apply(db.Model(&zero), scopes...)
	query = gormx.Paginate(q)(query)
	if err := query.Find(&items).Error; err != nil {
		return page, gormx.Translate(err, r.entity)
	}
	page.Items = items
	return page, nil
}

// All returns every matching row, unpaginated.
//
// Use it only where the result set is bounded by design — the regions of a
// platform, the rules of one security group. For anything a tenant can grow
// without limit, use List; "it is only a few hundred rows" is a statement
// about today.
func (r *Repository[T]) All(ctx context.Context, scopes ...gormx.Scope) ([]T, error) {
	db, err := r.Scoped(ctx)
	if err != nil {
		return nil, err
	}
	out := []T{}
	if err := gormx.Apply(db, scopes...).Find(&out).Error; err != nil {
		return nil, gormx.Translate(err, r.entity)
	}
	return out, nil
}

// Save writes every field of model, including the zero ones.
func (r *Repository[T]) Save(ctx context.Context, model *T) error {
	if err := r.DB(ctx).Save(model).Error; err != nil {
		return gormx.Translate(err, r.entity)
	}
	return nil
}

// UpdateFields applies a column→value map to the rows matching the scopes,
// within the tenant.
//
// A map, not a struct, because GORM skips zero-valued struct fields: updating
// with a struct can never set a column to false, 0 or "". That silent skip is
// a recurring production defect — a PATCH that disables something appears to
// succeed and changes nothing.
//
// It REFUSES an empty scope list, because an update with no WHERE rewrites
// every row the tenant owns.
func (r *Repository[T]) UpdateFields(ctx context.Context, fields map[string]any, scopes ...gormx.Scope) (int64, error) {
	if len(fields) == 0 {
		return 0, nil
	}
	if len(scopes) == 0 {
		return 0, errs.Internal("refusing to update every %s: no condition given", r.entity).
			WithCode("UNCONDITIONAL_UPDATE")
	}
	db, err := r.Scoped(ctx)
	if err != nil {
		return 0, err
	}
	var zero T
	res := gormx.Apply(db.Model(&zero), scopes...).Updates(fields)
	if res.Error != nil {
		return 0, gormx.Translate(res.Error, r.entity)
	}
	return res.RowsAffected, nil
}

// UpdateByID applies a column→value map to one row and reports NotFound when
// nothing matched — which covers both "no such id" and "not yours".
func (r *Repository[T]) UpdateByID(ctx context.Context, id uuid.UUID, fields map[string]any) error {
	n, err := r.UpdateFields(ctx, fields, gormx.Where("id = ?", id))
	if err != nil {
		return err
	}
	if n == 0 {
		return errs.NotFound("%s not found", r.entity).WithCode("NOT_FOUND")
	}
	return nil
}

// Delete removes rows matching the scopes (a soft delete when the entity has
// gorm.DeletedAt). It refuses an empty scope list for the same reason
// UpdateFields does.
func (r *Repository[T]) Delete(ctx context.Context, scopes ...gormx.Scope) (int64, error) {
	if len(scopes) == 0 {
		return 0, errs.Internal("refusing to delete every %s: no condition given", r.entity).
			WithCode("UNCONDITIONAL_DELETE")
	}
	db, err := r.Scoped(ctx)
	if err != nil {
		return 0, err
	}
	var zero T
	res := gormx.Apply(db, scopes...).Delete(&zero)
	if res.Error != nil {
		return 0, gormx.Translate(res.Error, r.entity)
	}
	return res.RowsAffected, nil
}

// DeleteByID removes one row, reporting NotFound when nothing matched.
func (r *Repository[T]) DeleteByID(ctx context.Context, id uuid.UUID) error {
	n, err := r.Delete(ctx, gormx.Where("id = ?", id))
	if err != nil {
		return err
	}
	if n == 0 {
		return errs.NotFound("%s not found", r.entity).WithCode("NOT_FOUND")
	}
	return nil
}

// LockByID loads one row FOR UPDATE. It must be called inside a transaction;
// a row lock outside one is released immediately and protects nothing.
func (r *Repository[T]) LockByID(ctx context.Context, id uuid.UUID) (*T, error) {
	if !InTx(ctx) {
		return nil, errs.Internal("LockByID on %s called outside a transaction", r.entity).
			WithCode("LOCK_OUTSIDE_TX")
	}
	return r.FindByID(ctx, id, gormx.ForUpdate())
}
