// Package filter is the last line of the request pipeline: it turns anything
// that escaped a handler into a response.
//
// Install ErrorHandler as Fiber's Config.ErrorHandler and no code path can
// return a bare Go error to a client.
//
// What it guarantees, which hand-written error handling does not:
//
//   - Every failure is logged ONCE, with its internal cause, under the request's
//     trace id — including the ones a handler returned without logging.
//   - No unclassified error's text reaches a client. An error that is not an
//     *errs.Error is by definition one nobody checked for leaks, so it becomes
//     a generic 500 and the original goes to the log.
//   - A 404 for an unrouted path looks like every other response, so a client's
//     error parsing does not have to special-case the framework's own replies.
package filter

import (
	"context"
	"errors"

	"github.com/gofiber/fiber/v3"

	"github.com/IncepTools/go/src/core/errs"
	"github.com/IncepTools/go/src/core/logx"
	"github.com/IncepTools/go/src/web/response"
)

// Filter handles errors of a shape it recognises.
//
// Catch returns handled=false to pass the error to the next filter. The chain
// always ends in the built-in behaviour, so a filter never has to handle
// everything.
type Filter interface {
	Name() string
	Catch(c fiber.Ctx, err error) (handled bool, writeErr error)
}

// Func adapts a plain function to Filter.
type Func struct {
	Label string
	Fn    func(c fiber.Ctx, err error) (bool, error)
}

// Name implements Filter.
func (f Func) Name() string { return f.Label }

// Catch implements Filter.
func (f Func) Catch(c fiber.Ctx, err error) (bool, error) { return f.Fn(c, err) }

// Of builds a Filter from a name and a function.
func Of(name string, fn func(c fiber.Ctx, err error) (bool, error)) Filter {
	return Func{Label: name, Fn: fn}
}

// Options configures the error handler.
type Options struct {
	// Filters run in order before the built-in mapping.
	Filters []Filter
	// LogClientErrors logs 4xx responses too. Off by default: a thousand
	// validation failures a minute is a client bug, and logging them buries
	// the server bugs.
	LogClientErrors bool
}

// ErrorHandler returns a handler for fiber.Config.ErrorHandler.
//
//	app := fiber.New(fiber.Config{ErrorHandler: filter.ErrorHandler(filter.Options{})})
func ErrorHandler(opt Options) func(fiber.Ctx, error) error {
	return func(c fiber.Ctx, err error) error {
		if err == nil {
			return nil
		}
		for _, f := range opt.Filters {
			if f == nil {
				continue
			}
			if handled, werr := f.Catch(c, err); handled {
				return werr
			}
		}
		return handle(c, err, opt)
	}
}

func handle(c fiber.Ctx, err error, opt Options) error {
	ctx := c.Context()

	// Fiber's own errors: an unrouted path, a method not allowed, a body over
	// the limit. These are not defects, and they must still come back in the
	// standard envelope.
	var fe *fiber.Error
	if errors.As(err, &fe) {
		mapped := fiberToErrs(fe)
		if opt.LogClientErrors || fe.Code >= 500 {
			logx.Err(ctx, "request failed", mapped, "status", fe.Code)
		}
		return response.Fail(c, mapped)
	}

	// A cancelled client is not a server failure. Logging it as one makes an
	// impatient user look like an outage.
	if errors.Is(err, context.Canceled) {
		return response.Fail(c, errs.Wrap(errs.KindInvalid, "the request was cancelled", err).
			WithCode("REQUEST_CANCELLED"))
	}
	if errors.Is(err, context.DeadlineExceeded) {
		logx.Err(ctx, "request timed out", err)
		return response.Fail(c, errs.Wrap(errs.KindTimeout, "the request timed out", err).
			WithCode("REQUEST_TIMEOUT"))
	}

	e := errs.From(err)
	kind := errs.KindOf(e)
	if opt.LogClientErrors || kind == errs.KindInternal || kind == errs.KindUnavailable {
		logx.Err(ctx, "request failed", e,
			"method", c.Method(), "route", c.Path(), "status", response.StatusFor(kind))
	}
	return response.Fail(c, e)
}

func fiberToErrs(fe *fiber.Error) *errs.Error {
	switch fe.Code {
	case fiber.StatusNotFound:
		return errs.NotFound("no route matches this path").WithCode("ROUTE_NOT_FOUND").WithCause(fe)
	case fiber.StatusMethodNotAllowed:
		return errs.Invalid("this method is not allowed on this path").
			WithCode("METHOD_NOT_ALLOWED").WithCause(fe)
	case fiber.StatusRequestEntityTooLarge:
		return errs.Invalid("the request body is too large").WithCode("BODY_TOO_LARGE").WithCause(fe)
	case fiber.StatusUnsupportedMediaType:
		return errs.Invalid("unsupported content type").WithCode("UNSUPPORTED_MEDIA_TYPE").WithCause(fe)
	case fiber.StatusTooManyRequests:
		return errs.Exhausted("too many requests").WithCode("RATE_LIMITED").WithCause(fe)
	case fiber.StatusUnauthorized:
		return errs.Unauthorized("authentication required").WithCode("UNAUTHENTICATED").WithCause(fe)
	case fiber.StatusForbidden:
		return errs.Forbidden("not permitted").WithCode("FORBIDDEN").WithCause(fe)
	}
	if fe.Code >= 500 {
		return errs.Wrap(errs.KindInternal, fe.Message, fe)
	}
	return errs.Wrap(errs.KindInvalid, fe.Message, fe)
}

// NotFoundHandler is the terminal handler for unmatched routes. Register it
// LAST, after every module: `app.Use(filter.NotFoundHandler())`.
func NotFoundHandler() fiber.Handler {
	return func(c fiber.Ctx) error {
		return response.Fail(c, errs.NotFound("no route matches this path").WithCode("ROUTE_NOT_FOUND"))
	}
}
