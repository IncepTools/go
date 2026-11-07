// Package interceptor wraps a handler on both sides: before it runs, after it
// returns, and around a failure.
//
// A guard answers yes or no; an interceptor observes and may change what
// happens — timing, caching, retries, a response header, an audit record
// written only when the write succeeded.
package interceptor

import (
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/IncepTools/go/src/core/logx"
)

// Interceptor wraps the rest of the chain.
//
// An implementation MUST call next exactly once on the paths where it intends
// the handler to run, and must return next's error unless it is deliberately
// replacing the outcome. Swallowing that error turns a failed write into a
// 200.
type Interceptor interface {
	Name() string
	Intercept(c fiber.Ctx, next fiber.Handler) error
}

// Func adapts a plain function to Interceptor.
type Func struct {
	Label string
	Wrap  func(c fiber.Ctx, next fiber.Handler) error
}

// Name implements Interceptor.
func (f Func) Name() string { return f.Label }

// Intercept implements Interceptor.
func (f Func) Intercept(c fiber.Ctx, next fiber.Handler) error { return f.Wrap(c, next) }

// Of builds an Interceptor from a name and a function.
func Of(name string, wrap func(c fiber.Ctx, next fiber.Handler) error) Interceptor {
	return Func{Label: name, Wrap: wrap}
}

// Use composes interceptors into one fiber middleware. The first listed is the
// outermost: it sees the request first and the response last.
func Use(is ...Interceptor) fiber.Handler {
	return func(c fiber.Ctx) error {
		next := func(c fiber.Ctx) error { return c.Next() }
		// Build inside-out so that is[0] ends up outermost.
		for i := len(is) - 1; i >= 0; i-- {
			ic := is[i]
			if ic == nil {
				continue
			}
			inner := next
			next = func(c fiber.Ctx) error { return ic.Intercept(c, inner) }
		}
		return next(c)
	}
}

// Timing records how long the handler took and logs anything slower than
// threshold at warn.
//
// It logs the slow ones only. A line per request is a metric wearing a log's
// clothing — it costs money to store and nobody reads it; a line for the 1%
// that were slow is read every time.
func Timing(threshold time.Duration) Interceptor {
	return Of("timing", func(c fiber.Ctx, next fiber.Handler) error {
		start := time.Now()
		err := next(c)
		elapsed := time.Since(start)
		if threshold > 0 && elapsed >= threshold {
			logx.C(c.Context()).Warn("slow request",
				"method", c.Method(), "route", c.Path(),
				"ms", elapsed.Milliseconds(), "thresholdMs", threshold.Milliseconds())
		}
		return err
	})
}

// Header sets a response header on the way out.
func Header(key, value string) Interceptor {
	return Of("header:"+key, func(c fiber.Ctx, next fiber.Handler) error {
		err := next(c)
		c.Set(key, value)
		return err
	})
}

// After runs fn once the handler has returned, with the handler's error.
//
// fn's own error is ignored on purpose: an audit write that fails must not
// turn a successful write into a 500 the client will retry.
func After(name string, fn func(c fiber.Ctx, handlerErr error)) Interceptor {
	return Of(name, func(c fiber.Ctx, next fiber.Handler) error {
		err := next(c)
		fn(c, err)
		return err
	})
}

// OnSuccess runs fn only when the handler returned no error. This is where an
// audit record or an outbox row belongs.
func OnSuccess(name string, fn func(c fiber.Ctx)) Interceptor {
	return After(name, func(c fiber.Ctx, err error) {
		if err == nil {
			fn(c)
		}
	})
}
