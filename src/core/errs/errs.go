// Package errs is the single error vocabulary of an inceptools application.
//
// A service layer never knows about HTTP. It returns an *errs.Error, and the
// transport decides what that becomes on the wire. There is no hierarchy of
// error types to learn — one concrete type with a machine-readable Code, a
// transport-neutral Kind, a caller-safe Message and an optional internal
// Cause that is never serialized.
//
// The rule that makes this worth having: Message is the only field that reaches
// a client. Anything you must not leak — a driver string, an upstream reply, a
// query — goes in Cause, where it reaches the log and nothing else.
package errs

import (
	"errors"
	"fmt"
)

// Kind is the transport-neutral class of a failure. Adapters map it to their
// own status vocabulary (web/response maps it to HTTP).
type Kind string

// The kinds. Every *Error has one; the zero value is treated as KindInternal.
const (
	KindInvalid      Kind = "invalid"       // malformed or failed validation
	KindUnauthorized Kind = "unauthorized"  // not authenticated
	KindForbidden    Kind = "forbidden"     // authenticated, not permitted
	KindNotFound     Kind = "not_found"     // no such resource, or not visible to this tenant
	KindConflict     Kind = "conflict"      // state collision, uniqueness, concurrent update
	KindExhausted    Kind = "exhausted"     // quota or rate limit
	KindPayment      Kind = "payment"       // payment or credit required
	KindUnavailable  Kind = "unavailable"   // dependency down, retry may succeed
	KindTimeout      Kind = "timeout"       // deadline exceeded
	KindNotSupported Kind = "not_supported" // never going to work here
	KindInternal     Kind = "internal"      // a defect; the default for an unclassified error
)

// Detail is one field-scoped explanation attached to an error. Validation
// failures are the common producer, but any error may carry them.
type Detail struct {
	Field   string `json:"field"`
	Message string `json:"message"`
	Rule    string `json:"rule,omitempty"`
}

// Error is the one error type inceptools code returns across a layer boundary.
type Error struct {
	// Kind decides the transport status. Always set; zero value is treated as
	// KindInternal.
	Kind Kind
	// Code is a stable machine identifier a client may branch on, such as
	// "SKU_ALREADY_TAKEN". Optional, but strongly preferred over parsing Message.
	Code string
	// Message is caller-safe prose. It is the ONLY field that reaches a client.
	Message string
	// Details carry per-field explanations.
	Details []Detail
	// Cause is the internal error. Logged, never serialized.
	Cause error
	// Meta carries structured context for the log — ids, names, counts. Like
	// Cause, it does not reach the client.
	Meta map[string]any
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	switch {
	case e.Cause != nil && e.Message != "":
		return e.Message + ": " + e.Cause.Error()
	case e.Cause != nil:
		return string(e.kind()) + ": " + e.Cause.Error()
	case e.Message != "":
		return e.Message
	default:
		return string(e.kind())
	}
}

// Unwrap exposes Cause to errors.Is and errors.As.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *Error) kind() Kind {
	if e == nil || e.Kind == "" {
		return KindInternal
	}
	return e.Kind
}

// WithCode returns a copy carrying a machine-readable code.
func (e *Error) WithCode(code string) *Error { c := e.clone(); c.Code = code; return c }

// WithCause returns a copy carrying an internal cause. The cause never reaches
// a client.
func (e *Error) WithCause(err error) *Error { c := e.clone(); c.Cause = err; return c }

// WithDetails returns a copy carrying field-scoped details.
func (e *Error) WithDetails(d ...Detail) *Error {
	c := e.clone()
	c.Details = append(append([]Detail(nil), c.Details...), d...)
	return c
}

// WithMeta returns a copy carrying one structured log field.
func (e *Error) WithMeta(key string, value any) *Error {
	c := e.clone()
	if c.Meta == nil {
		c.Meta = map[string]any{}
	} else {
		m := make(map[string]any, len(c.Meta)+1)
		for k, v := range c.Meta {
			m[k] = v
		}
		c.Meta = m
	}
	c.Meta[key] = value
	return c
}

func (e *Error) clone() *Error {
	if e == nil {
		return &Error{Kind: KindInternal}
	}
	c := *e
	return &c
}

func newf(kind Kind, format string, args ...any) *Error {
	msg := format
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	}
	return &Error{Kind: kind, Message: msg}
}

// Invalid reports a malformed request or a failed business rule.
func Invalid(format string, args ...any) *Error { return newf(KindInvalid, format, args...) }

// Unauthorized reports a missing or unusable credential.
func Unauthorized(format string, args ...any) *Error {
	return newf(KindUnauthorized, format, args...)
}

// Forbidden reports an authenticated caller without the permission.
func Forbidden(format string, args ...any) *Error { return newf(KindForbidden, format, args...) }

// NotFound reports a resource that does not exist OR is not visible to this
// tenant. Returning Forbidden for the second case confirms the resource exists
// to a caller who should not know; prefer NotFound for both.
func NotFound(format string, args ...any) *Error { return newf(KindNotFound, format, args...) }

// Conflict reports a state collision.
func Conflict(format string, args ...any) *Error { return newf(KindConflict, format, args...) }

// Exhausted reports a quota or rate limit.
func Exhausted(format string, args ...any) *Error { return newf(KindExhausted, format, args...) }

// PaymentRequired reports insufficient credit or a declined charge.
func PaymentRequired(format string, args ...any) *Error { return newf(KindPayment, format, args...) }

// Unavailable reports a dependency that is down. Retrying may succeed.
func Unavailable(format string, args ...any) *Error { return newf(KindUnavailable, format, args...) }

// Timeout reports a deadline exceeded.
func Timeout(format string, args ...any) *Error { return newf(KindTimeout, format, args...) }

// NotSupported reports something this deployment will never do.
func NotSupported(format string, args ...any) *Error {
	return newf(KindNotSupported, format, args...)
}

// Internal reports a defect. The message is NOT shown to clients by the web
// layer; it is replaced with a generic sentence and logged under the trace id.
func Internal(format string, args ...any) *Error { return newf(KindInternal, format, args...) }

// Wrap turns any error into an *Error of the given kind, preserving the
// original as Cause. A nil error wraps to nil, so it is safe to write
// `return errs.Wrap(errs.KindInternal, "x", err)` on a possibly-nil err.
func Wrap(kind Kind, message string, err error) *Error {
	if err == nil {
		return nil
	}
	return &Error{Kind: kind, Message: message, Cause: err}
}

// From normalises any error into an *Error. An error that is already one (or
// wraps one) is returned as-is; anything else becomes KindInternal with the
// original as Cause, so it is logged but not leaked.
func From(err error) *Error {
	if err == nil {
		return nil
	}
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return &Error{Kind: KindInternal, Message: "internal error", Cause: err}
}

// KindOf reports the kind of any error, defaulting to KindInternal.
func KindOf(err error) Kind {
	if err == nil {
		return ""
	}
	var e *Error
	if errors.As(err, &e) {
		return e.kind()
	}
	return KindInternal
}

// Is reports whether err is (or wraps) an *Error of the given kind.
func Is(err error, kind Kind) bool { return err != nil && KindOf(err) == kind }

// CodeOf returns the machine code of err, or "" when it carries none.
func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}
