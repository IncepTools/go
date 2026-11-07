// Package service holds the notifications module's business rules.
//
// Note what it does NOT import: nothing from web/, and no mailer or audit
// client. It publishes a fact — the order was placed — and whoever cares
// subscribes. That is what lets a second consequence be added without this
// file changing, which is the whole argument for a bus over a direct call.
package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/IncepTools/go/examples/event-emitter/apps/events/notifications/dto"
	"github.com/IncepTools/go/src/core/errs"
	"github.com/IncepTools/go/src/core/logx"
	"github.com/IncepTools/go/src/platform/events"
)

// OrderPlaced is published once an order is accepted.
//
// It carries what a subscriber needs rather than a bare id, so a handler does
// not have to call back into this service to do its job. It must stay
// serialisable and small: an event is a fact, not a pointer to one.
type OrderPlaced struct {
	OrderID    uuid.UUID
	Reference  string
	Email      string
	TotalCents int64
}

// OrderService accepts orders and announces them.
type OrderService struct {
	bus *events.Bus
}

// NewOrderService builds the service.
func NewOrderService(bus *events.Bus) *OrderService { return &OrderService{bus: bus} }

// Place accepts the order and publishes the fact.
//
// The stock check happens HERE, before the publish, and that placement is the
// point of the example. Publish dispatches every async subscriber as it walks
// the list, so by the time a Sync subscriber returns an error the async ones
// have already been started — a Sync subscriber can report a failure but it
// cannot veto the event. Anything that must be able to refuse the operation
// is a step in the service, not a subscriber.
//
// Get that backwards and the order is rejected with a 409 while the customer
// still receives a confirmation email.
func (s *OrderService) Place(ctx context.Context, req dto.PlaceOrder) (dto.OrderReceipt, error) {
	if err := s.reserveStock(ctx, req); err != nil {
		return dto.OrderReceipt{}, err
	}

	id := uuid.New()
	if err := events.Publish(ctx, s.bus, OrderPlaced{
		OrderID:    id,
		Reference:  req.Reference,
		Email:      req.Email,
		TotalCents: req.TotalCents,
	}); err != nil {
		return dto.OrderReceipt{}, err
	}
	return dto.OrderReceipt{ID: id.String(), Reference: req.Reference}, nil
}

// reserveStock is a prerequisite, not a consequence. The order must not exist
// if it fails, so it runs before anything is announced.
func (s *OrderService) reserveStock(ctx context.Context, req dto.PlaceOrder) error {
	if req.TotalCents > 1_000_000 {
		return errs.Conflict("not enough stock for order %s", req.Reference).
			WithCode("OUT_OF_STOCK")
	}
	logx.C(ctx).Info("stock reserved", "reference", req.Reference)
	return nil
}
