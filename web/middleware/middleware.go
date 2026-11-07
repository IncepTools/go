// Package middleware is the request-scoped plumbing every route needs before
// any handler runs: identity on the context, a trace id, a recovered panic, a
// deadline.
//
// Order is not a style choice here. RequestID must precede Recover so a panic
// is logged with the id the client was given; Recover must precede everything
// that can panic; Timeout must precede the handler, not the logging. Chain
// gives you the order that is correct, and is what you should use unless you
// know why you are deviating.
package middleware

import (
	"context"
	"fmt"
	"runtime/debug"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/IncepTools/go/core/ctxkit"
	"github.com/IncepTools/go/core/errs"
	"github.com/IncepTools/go/core/logx"
	"github.com/IncepTools/go/web/response"
)

// HeaderRequestID is the inbound/outbound correlation header.
const HeaderRequestID = "X-Request-Id"

// HeaderTraceID is the header a client quotes in a support ticket.
const HeaderTraceID = "X-Trace-Id"

// RequestID establishes the correlation id for the request and puts it on the
// Fiber locals, on the Go context and on the response.
//
// It TRUSTS an inbound X-Request-Id, which is what makes a trace span a
// gateway, a service and a worker. That trust is only safe behind your own
// edge: a public listener should strip the header there, or this becomes a
// log-injection vector.
func RequestID() fiber.Handler {
	return func(c fiber.Ctx) error {
		id := strings.TrimSpace(c.Get(HeaderRequestID))
		if id == "" || len(id) > 200 {
			id = uuid.NewString()
		}
		c.Locals(response.LocalTraceID, id)
		c.Locals(response.LocalStartKey, time.Now())
		c.Set(HeaderRequestID, id)
		c.Set(HeaderTraceID, id)

		ctx := ctxkit.WithTrace(c.Context(), id)
		ctx = ctxkit.WithRequestID(ctx, id)
		c.SetContext(ctx)
		return c.Next()
	}
}

// Recover turns a panic into a 500 in the standard envelope, with the stack in
// the log and nothing of it on the wire.
//
// A panic that escapes to Fiber's own recovery loses the trace id and the
// request context, which is exactly the information the incident needs.
func Recover() fiber.Handler {
	return func(c fiber.Ctx) (err error) {
		defer func() {
			if r := recover(); r != nil {
				stack := string(debug.Stack())
				logx.C(c.Context()).Error("panic recovered",
					"panic", fmt.Sprint(r),
					"method", c.Method(),
					"route", c.Path(),
					"stack", stack,
				)
				err = response.InternalError(c)
			}
		}()
		return c.Next()
	}
}

// Timeout bounds a handler.
//
// It sets a deadline on the context; it cannot stop a handler that ignores it.
// That is a real limit, not an oversight — Go has no way to kill a goroutine —
// so the value of this middleware is entirely in handlers and drivers that
// honour ctx. Code that performs I/O without a context is the thing to fix.
func Timeout(d time.Duration) fiber.Handler {
	return func(c fiber.Ctx) error {
		if d <= 0 {
			return c.Next()
		}
		ctx, cancel := context.WithTimeout(c.Context(), d)
		defer cancel()
		c.SetContext(ctx)
		err := c.Next()
		// A handler that returned successfully AFTER its deadline passed has
		// produced a result nobody is waiting for; report the timeout
		// instead. When it returned an error, that error is the better one.
		if err == nil && ctx.Err() == context.DeadlineExceeded {
			return response.Fail(c, errs.Timeout("the request took too long").WithCode("REQUEST_TIMEOUT"))
		}
		return err //nolint:nilerr // err is returned as-is, nil included
	}
}

// AccessLogOptions configures AccessLog.
type AccessLogOptions struct {
	// SkipPaths are not logged. Put the health and metrics endpoints here:
	// a liveness probe every two seconds is 43,200 log lines a day saying
	// nothing happened.
	SkipPaths []string
	// SlowThreshold promotes a slow request to warn. Zero disables it.
	SlowThreshold time.Duration
	// LogSuccess logs 2xx and 3xx as well. Off by default — the access log of
	// a busy service is a metric, and metrics belong in a metrics system.
	LogSuccess bool
}

// AccessLog writes one line per request that failed, was slow, or — when
// LogSuccess is set — per request at all.
func AccessLog(opt AccessLogOptions) fiber.Handler {
	skip := make(map[string]struct{}, len(opt.SkipPaths))
	for _, p := range opt.SkipPaths {
		skip[p] = struct{}{}
	}
	return func(c fiber.Ctx) error {
		if _, ok := skip[c.Path()]; ok {
			return c.Next()
		}
		start := time.Now()
		err := c.Next()
		elapsed := time.Since(start)
		status := c.Response().StatusCode()

		slow := opt.SlowThreshold > 0 && elapsed >= opt.SlowThreshold
		if !opt.LogSuccess && status < 400 && !slow && err == nil {
			return nil
		}
		args := []any{
			"method", c.Method(),
			"route", c.Path(),
			"status", status,
			"ms", elapsed.Milliseconds(),
			"ip", c.IP(),
		}
		l := logx.C(c.Context())
		switch {
		case status >= 500 || err != nil:
			l.Error("request", args...)
		case status >= 400 || slow:
			l.Warn("request", args...)
		default:
			l.Info("request", args...)
		}
		return err
	}
}

// IdentityResolver extracts the tenant and actor from a request. Return an
// error to refuse the request outright; return zero values to let it proceed
// anonymously and have a guard decide.
type IdentityResolver func(c fiber.Ctx) (ctxkit.Tenant, ctxkit.Actor, error)

// Identity puts the tenant and actor on the Go context, where every layer
// below reads them from.
//
// It does NOT decide whether the request is allowed — that is a guard's job.
// Keeping the two apart is what lets an endpoint be public and still know who
// the caller is when they happen to be signed in.
func Identity(resolve IdentityResolver) fiber.Handler {
	return func(c fiber.Ctx) error {
		tenant, actor, err := resolve(c)
		if err != nil {
			return response.Fail(c, err)
		}
		ctx := c.Context()
		if !tenant.Zero() {
			ctx = ctxkit.WithTenant(ctx, tenant)
		}
		if actor.UserID != uuid.Nil {
			ctx = ctxkit.WithActor(ctx, actor)
		}
		c.SetContext(ctx)
		return c.Next()
	}
}

// SecureHeadersOptions configures SecureHeaders.
type SecureHeadersOptions struct {
	// HSTSMaxAge enables Strict-Transport-Security. Zero disables it — do not
	// send HSTS from a service reachable over plain HTTP in development, or a
	// developer's browser will pin localhost to HTTPS and they will lose an
	// afternoon to it.
	HSTSMaxAge time.Duration
	// ContentSecurityPolicy is sent verbatim when non-empty. The default below
	// suits a JSON API; a service that serves HTML needs its own.
	ContentSecurityPolicy string
	// FrameOptions defaults to DENY.
	FrameOptions string
}

// SecureHeaders sets the response headers that cost nothing and close a
// category of attack each.
func SecureHeaders(opt SecureHeadersOptions) fiber.Handler {
	frame := opt.FrameOptions
	if frame == "" {
		frame = "DENY"
	}
	csp := opt.ContentSecurityPolicy
	if csp == "" {
		csp = "default-src 'none'; frame-ancestors 'none'"
	}
	var hsts string
	if opt.HSTSMaxAge > 0 {
		hsts = fmt.Sprintf("max-age=%d; includeSubDomains", int(opt.HSTSMaxAge.Seconds()))
	}
	return func(c fiber.Ctx) error {
		c.Set("X-Content-Type-Options", "nosniff")
		c.Set("X-Frame-Options", frame)
		c.Set("Referrer-Policy", "no-referrer")
		c.Set("Content-Security-Policy", csp)
		c.Set("Cross-Origin-Opener-Policy", "same-origin")
		c.Set("Cross-Origin-Resource-Policy", "same-origin")
		c.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		if hsts != "" {
			c.Set("Strict-Transport-Security", hsts)
		}
		return c.Next()
	}
}

// ChainOptions configures Chain.
type ChainOptions struct {
	Timeout       time.Duration
	AccessLog     AccessLogOptions
	SecureHeaders *SecureHeadersOptions
	Identity      IdentityResolver
}

// Chain returns the standard middleware stack in the order that is correct.
//
// RequestID first so everything after it can log the id. Recover second so a
// panic anywhere below is caught WITH that id. Headers and logging next.
// Identity before Timeout, so an authentication failure is not reported as a
// timeout. The handler last.
func Chain(opt ChainOptions) []fiber.Handler {
	out := []fiber.Handler{RequestID(), Recover()}
	if opt.SecureHeaders != nil {
		out = append(out, SecureHeaders(*opt.SecureHeaders))
	}
	out = append(out, AccessLog(opt.AccessLog))
	if opt.Identity != nil {
		out = append(out, Identity(opt.Identity))
	}
	if opt.Timeout > 0 {
		out = append(out, Timeout(opt.Timeout))
	}
	return out
}
