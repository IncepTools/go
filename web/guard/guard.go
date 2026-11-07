// Package guard is authorization: the question "may this caller do this?",
// asked once per route, before the handler runs.
//
// A guard returns nil to allow and an *errs.Error to refuse, and the refusal
// carries its own status — so a guard that needs to answer 402 rather than
// 403 just says so.
//
// Why a guard and not an `if` at the top of the handler: a route is declared
// in one line, and the guards on it are part of that line. A reviewer reading
// the module's Register function can see what protects each route without
// opening six handlers, and a route added without protection is visibly
// different from its neighbours.
package guard

import (
	"github.com/gofiber/fiber/v3"

	"github.com/IncepTools/go/core/ctxkit"
	"github.com/IncepTools/go/core/errs"
	"github.com/IncepTools/go/web/response"
)

// Guard decides whether a request may proceed.
//
// Returning nil allows it. Returning an error refuses it, and the error's kind
// picks the status — use errs.Unauthorized for "who are you", errs.Forbidden
// for "not you", errs.PaymentRequired or errs.Exhausted where those fit.
type Guard interface {
	// Name identifies the guard in logs and in the route table.
	Name() string
	CanActivate(c fiber.Ctx) error
}

// Func adapts a plain function to Guard.
type Func struct {
	Label string
	Check func(c fiber.Ctx) error
}

// Name implements Guard.
func (f Func) Name() string { return f.Label }

// CanActivate implements Guard.
func (f Func) CanActivate(c fiber.Ctx) error { return f.Check(c) }

// Of builds a Guard from a name and a function.
func Of(name string, check func(c fiber.Ctx) error) Guard { return Func{Label: name, Check: check} }

// Use returns a fiber handler that runs the guards in order and stops at the
// first refusal.
//
// Order is yours to choose and it matters: put the cheap checks first. An
// authentication guard that reads a cached token should run before a quota
// guard that queries a database, so an unauthenticated flood costs a map
// lookup rather than a connection.
func Use(guards ...Guard) fiber.Handler {
	return func(c fiber.Ctx) error {
		for _, g := range guards {
			if g == nil {
				continue
			}
			if err := g.CanActivate(c); err != nil {
				return response.Fail(c, annotate(err, g.Name()))
			}
		}
		return c.Next()
	}
}

func annotate(err error, guardName string) error {
	e := errs.From(err)
	if e.Meta == nil || e.Meta["guard"] == nil {
		return e.WithMeta("guard", guardName)
	}
	return e
}

// All passes only when every guard passes. It is the same as listing them,
// and exists so a set of guards can be named once and reused.
func All(name string, guards ...Guard) Guard {
	return Of(name, func(c fiber.Ctx) error {
		for _, g := range guards {
			if err := g.CanActivate(c); err != nil {
				return err
			}
		}
		return nil
	})
}

// Any passes when at least one guard passes, and reports the LAST refusal when
// none do.
//
// Use it for "an owner or an admin may do this". Do not use it to paper over a
// guard that is failing for a reason you have not understood.
func Any(name string, guards ...Guard) Guard {
	return Of(name, func(c fiber.Ctx) error {
		var last error
		for _, g := range guards {
			err := g.CanActivate(c)
			if err == nil {
				return nil
			}
			last = err
		}
		if last == nil {
			return errs.Forbidden("not permitted").WithCode("FORBIDDEN")
		}
		return last
	})
}

// Authenticated refuses a request that carries no actor.
//
// It reads what the authentication middleware put on the context; it does not
// itself verify a token. Keeping the two apart means a test can establish an
// actor without minting a credential.
var Authenticated = Of("authenticated", func(c fiber.Ctx) error {
	if ctxkit.ActorOf(c.Context()).UserID.String() == "00000000-0000-0000-0000-000000000000" {
		return errs.Unauthorized("authentication required").WithCode("UNAUTHENTICATED")
	}
	return nil
})

// Tenanted refuses a request that is not scoped to an account.
//
// Every tenant-owned read and write depends on this having run: a zero account
// id reaching a repository is the bug that returns another customer's rows.
var Tenanted = Of("tenanted", func(c fiber.Ctx) error {
	if ctxkit.TenantOf(c.Context()).Zero() {
		return errs.Unauthorized("no account context on this request").WithCode("NO_TENANT")
	}
	return nil
})

// HasRole refuses a caller lacking any of the listed roles. A platform
// operator is NOT automatically allowed; say so explicitly with Any if that is
// what you mean.
func HasRole(roles ...string) Guard {
	return Of("role:"+join(roles), func(c fiber.Ctx) error {
		actor := ctxkit.ActorOf(c.Context())
		for _, r := range roles {
			if actor.HasRole(r) {
				return nil
			}
		}
		return errs.Forbidden("this action requires one of: %s", join(roles)).WithCode("ROLE_REQUIRED")
	})
}

// Operator allows only a platform super-admin.
var Operator = Of("operator", func(c fiber.Ctx) error {
	if !ctxkit.TenantOf(c.Context()).Operator {
		// Deliberately NotFound-shaped prose rather than "you are not an
		// admin": an operator-only route should not advertise itself.
		return errs.Forbidden("not permitted").WithCode("OPERATOR_ONLY")
	}
	return nil
})

func join(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += ", "
		}
		out += v
	}
	return out
}
