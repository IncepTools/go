// Package ctxkit carries request-scoped identity on a context.Context.
//
// It exists so that a service, a repository, a background goroutine and a log
// line all read the same trace id and the same tenant from the same place. The
// keys are unexported: nothing outside this package can collide with them, and
// nothing can put a value of the wrong type under one.
package ctxkit

import (
	"context"

	"github.com/google/uuid"
)

type key int

const (
	keyTrace key = iota
	keyTenant
	keyActor
	keyRequest
)

// Tenant is the ownership boundary every stored row is scoped by. A zero
// AccountID means "no tenant" — a platform task, a reconciler, a migration —
// and a repository MUST refuse to treat that as "all tenants".
type Tenant struct {
	AccountID uuid.UUID
	// Operator is true for a platform super-admin. It does not widen the
	// tenant scope on its own; a caller that wants cross-tenant reach asks for
	// it explicitly.
	Operator bool
}

// Zero reports whether t names no tenant.
func (t Tenant) Zero() bool { return t.AccountID == uuid.Nil }

// Actor is who made the request, inside the tenant.
type Actor struct {
	UserID   uuid.UUID
	DeviceID string
	// Roles are opaque strings; guards interpret them.
	Roles []string
}

// HasRole reports whether the actor carries role.
func (a Actor) HasRole(role string) bool {
	for _, r := range a.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// WithTrace returns a context carrying a correlation id.
func WithTrace(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, keyTrace, traceID)
}

// Trace returns the correlation id, or "" when none was set.
func Trace(ctx context.Context) string {
	v, _ := ctx.Value(keyTrace).(string)
	return v
}

// WithTenant returns a context scoped to one tenant.
func WithTenant(ctx context.Context, t Tenant) context.Context {
	return context.WithValue(ctx, keyTenant, t)
}

// TenantOf returns the tenant, zero when none was set.
func TenantOf(ctx context.Context) Tenant {
	v, _ := ctx.Value(keyTenant).(Tenant)
	return v
}

// WithActor returns a context carrying the acting principal.
func WithActor(ctx context.Context, a Actor) context.Context {
	return context.WithValue(ctx, keyActor, a)
}

// ActorOf returns the acting principal, zero when none was set.
func ActorOf(ctx context.Context) Actor {
	v, _ := ctx.Value(keyActor).(Actor)
	return v
}

// WithRequestID returns a context carrying the per-request id.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, keyRequest, id)
}

// RequestID returns the per-request id, or "" when none was set.
func RequestID(ctx context.Context) string {
	v, _ := ctx.Value(keyRequest).(string)
	return v
}

// Detach returns a context that keeps the trace, tenant, actor and request id
// of parent but NOT its cancellation or deadline.
//
// This is the only correct way to hand work to a goroutine that outlives the
// request that started it. Passing the request context directly cancels the
// background work the moment the client disconnects — which is how
// fire-and-forget background work silently stops half-done.
func Detach(parent context.Context) context.Context {
	ctx := context.WithoutCancel(parent)
	return ctx
}
