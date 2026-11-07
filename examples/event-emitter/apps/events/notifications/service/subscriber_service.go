package service

import (
	"context"
	"time"

	"github.com/IncepTools/go/src/core/logx"
)

// SubscriberService is the consuming half: one method per consequence.
//
// They are methods rather than closures in the module so each one can be
// tested by calling it, with no bus and no HTTP request involved.
//
// Every one of them is a consequence of an order that already exists. None
// can refuse it — see OrderService.Place for why that is a property of the
// bus and not a style choice.
type SubscriberService struct{}

// NewSubscriberService builds the consuming half.
func NewSubscriberService() *SubscriberService { return &SubscriberService{} }

// NotifyCustomer runs ASYNC: a slow mail provider must not slow the response,
// and a failing one must not fail an order that was accepted correctly.
//
// The bus hands an async handler a DETACHED context: it keeps the trace id
// and the tenant, so these logs still join the request, but it drops the
// deadline and the cancellation — a side effect must not die because the
// client hung up. Shutdown still waits for it, because the bus tracks
// in-flight handlers.
func (s *SubscriberService) NotifyCustomer(ctx context.Context, e OrderPlaced) error {
	time.Sleep(300 * time.Millisecond)
	logx.C(ctx).Info("confirmation sent", "orderId", e.OrderID.String(), "email", e.Email)
	return nil
}

// Audit runs ASYNC: nobody is waiting on it and nothing depends on it.
//
// An async handler's error is logged by the bus and reaches no caller, which
// is the trade being made: the request stays fast, and the failure is
// something you find in the logs rather than in the response.
func (s *SubscriberService) Audit(ctx context.Context, e OrderPlaced) error {
	logx.C(ctx).Info("order audited",
		"orderId", e.OrderID.String(), "reference", e.Reference, "totalCents", e.TotalCents)
	return nil
}
