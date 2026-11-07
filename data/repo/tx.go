package repo

import (
	"context"

	"gorm.io/gorm"

	"github.com/IncepTools/go/core/errs"
)

type txKey struct{}

// TxFrom returns the transaction on ctx, or fallback when there is none.
//
// Every repository method routes through this, which is what makes
// transactions composable: a service calls three repositories inside one
// Transaction block and all three join it, without any of them taking a *gorm.DB
// parameter or knowing a transaction exists.
func TxFrom(ctx context.Context, fallback *gorm.DB) *gorm.DB {
	if tx, ok := ctx.Value(txKey{}).(*gorm.DB); ok && tx != nil {
		return tx
	}
	return fallback
}

// WithTx puts a transaction on the context.
func WithTx(ctx context.Context, tx *gorm.DB) context.Context {
	return context.WithValue(ctx, txKey{}, tx)
}

// InTx reports whether ctx is inside a transaction.
func InTx(ctx context.Context) bool {
	tx, ok := ctx.Value(txKey{}).(*gorm.DB)
	return ok && tx != nil
}

// Transaction runs fn inside one database transaction.
//
// fn receives a context carrying the transaction, so every repository call it
// makes joins that transaction automatically. Return an error and everything
// rolls back; return nil and it commits.
//
// Nesting is handled by JOINING the outer transaction rather than opening a
// savepoint. That is the behaviour a service wants: when a charge inside a
// provisioning transaction fails, the resource row must roll back with it,
// and a savepoint would let the outer work commit without the charge.
func Transaction(ctx context.Context, db *gorm.DB, fn func(ctx context.Context) error) error {
	if InTx(ctx) {
		return fn(ctx)
	}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(WithTx(ctx, tx))
	})
	if err != nil {
		// An *errs.Error from fn passes through untouched — the service
		// already said what went wrong, and re-wrapping it as a database
		// failure would lose the kind and turn a 409 into a 500.
		if e := errs.From(err); errs.KindOf(e) != errs.KindInternal {
			return err
		}
		return errs.Wrap(errs.KindInternal, "the transaction failed", err)
	}
	return nil
}

// TxManager runs transactions against one database. Register it in the
// container so a service can depend on "a way to run a transaction" without
// depending on *gorm.DB.
type TxManager struct{ db *gorm.DB }

// NewTxManager builds a manager.
func NewTxManager(db *gorm.DB) *TxManager { return &TxManager{db: db} }

// Do runs fn in a transaction.
func (m *TxManager) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	return Transaction(ctx, m.db, fn)
}
