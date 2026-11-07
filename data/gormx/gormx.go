// Package gormx translates database errors into the application's error
// vocabulary, and provides the query scopes every repository needs.
//
// The translation is the valuable half. A driver error reaching a handler has
// two failure modes, and both are real: it is rendered as a 500 when it was
// actually a duplicate name the user can fix, and its text — containing the
// constraint name, the table and sometimes the value — is sent to the client.
package gormx

import (
	"errors"
	"strings"

	"gorm.io/gorm"

	"github.com/IncepTools/go/core/errs"
	"github.com/IncepTools/go/web/pagination"
)

// PostgreSQL error codes worth distinguishing. They arrive inside the driver's
// error text, which is why this matches on strings: importing pgconn to type-
// assert would bind the framework to one driver, and the framework is used
// with more than one.
const (
	codeUniqueViolation     = "23505"
	codeForeignKeyViolation = "23503"
	codeNotNullViolation    = "23502"
	codeCheckViolation      = "23514"
	codeSerializationFail   = "40001"
	codeDeadlock            = "40P01"
	codeLockNotAvailable    = "55P03"
)

// Translate maps a database error to an *errs.Error, with the original kept
// as the cause so the log still has everything.
//
// entity names the thing being operated on, for the message: "product", not
// "ProductModel" and not "catalog_products".
func Translate(err error, entity string) error {
	if err == nil {
		return nil
	}
	if entity == "" {
		entity = "record"
	}

	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return errs.NotFound("%s not found", entity).WithCode("NOT_FOUND").WithCause(err)
	case errors.Is(err, gorm.ErrDuplicatedKey):
		return errs.Conflict("a %s with these details already exists", entity).
			WithCode("ALREADY_EXISTS").WithCause(err)
	case errors.Is(err, gorm.ErrForeignKeyViolated):
		return errs.Conflict("this %s is still referenced by something else", entity).
			WithCode("STILL_REFERENCED").WithCause(err)
	case errors.Is(err, gorm.ErrInvalidTransaction):
		return errs.Internal("invalid transaction").WithCause(err)
	}

	text := err.Error()
	switch {
	case strings.Contains(text, codeUniqueViolation):
		return errs.Conflict("a %s with these details already exists", entity).
			WithCode("ALREADY_EXISTS").WithCause(err)
	case strings.Contains(text, codeForeignKeyViolation):
		return errs.Conflict("this %s is still referenced by something else", entity).
			WithCode("STILL_REFERENCED").WithCause(err)
	case strings.Contains(text, codeNotNullViolation):
		return errs.Invalid("a required field on the %s is missing", entity).
			WithCode("MISSING_FIELD").WithCause(err)
	case strings.Contains(text, codeCheckViolation):
		return errs.Invalid("the %s violates a constraint", entity).
			WithCode("CONSTRAINT_VIOLATED").WithCause(err)
	case strings.Contains(text, codeSerializationFail), strings.Contains(text, codeDeadlock):
		// Retryable. Say so with a kind the caller can branch on rather than
		// burying it in a 500 that a client will retry blindly anyway.
		return errs.Conflict("the %s was modified concurrently; retry", entity).
			WithCode("CONCURRENT_MODIFICATION").WithCause(err)
	case strings.Contains(text, codeLockNotAvailable):
		return errs.Conflict("the %s is locked by another operation", entity).
			WithCode("LOCKED").WithCause(err)
	}

	return errs.Wrap(errs.KindInternal, "the database request failed", err).WithMeta("entity", entity)
}

// IsNotFound reports whether err is GORM's not-found.
func IsNotFound(err error) bool { return errors.Is(err, gorm.ErrRecordNotFound) }

// Scope is a composable query fragment.
type Scope func(*gorm.DB) *gorm.DB

// Apply runs scopes against a query.
func Apply(db *gorm.DB, scopes ...Scope) *gorm.DB {
	for _, s := range scopes {
		if s != nil {
			db = s(db)
		}
	}
	return db
}

// Paginate applies LIMIT, OFFSET and ORDER BY from a parsed page request.
//
// The ORDER BY is only safe because pagination.Query.Sort is restricted to an
// allow-list at parse time. Never hand this a sort key straight off a request.
func Paginate(q pagination.Query) Scope {
	return func(db *gorm.DB) *gorm.DB {
		db = db.Limit(q.Limit).Offset(q.Offset())
		if order := q.OrderBy(); order != "" {
			db = db.Order(order)
		}
		return db
	}
}

// Where is a plain condition as a scope.
func Where(query any, args ...any) Scope {
	return func(db *gorm.DB) *gorm.DB { return db.Where(query, args...) }
}

// Preload eager-loads an association.
//
// Reach for it whenever a list handler reads an association per row. That
// pattern — the N+1 — is the single most common performance defect in this
// kind of service, and it never shows up in development, where the list has
// four rows.
func Preload(assoc string, args ...any) Scope {
	return func(db *gorm.DB) *gorm.DB { return db.Preload(assoc, args...) }
}

// Order sorts by a column YOU name in code. Not for request input.
func Order(clause string) Scope {
	return func(db *gorm.DB) *gorm.DB { return db.Order(clause) }
}

// ForUpdate takes a row lock for the rest of the transaction.
//
// Use it for read-modify-write on a counter, a quota or an allocation. The
// alternative — read, decide, write — is a lost update under any concurrency
// at all, and the window is wide enough to hit in practice.
func ForUpdate() Scope {
	return func(db *gorm.DB) *gorm.DB { return db.Clauses(lockForUpdate{}) }
}
