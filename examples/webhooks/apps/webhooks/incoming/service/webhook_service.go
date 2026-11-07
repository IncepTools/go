// Package service holds the incoming module's business rules.
//
// Note what it does NOT import: nothing from web/. Verification, replay
// detection and dispatch take a context and plain arguments, so the same
// logic serves an HTTP delivery, a replay tool and a test.
package service

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/IncepTools/go/examples/webhooks/apps/webhooks/incoming/dto"
	"github.com/IncepTools/go/src/core/errs"
	"github.com/IncepTools/go/src/platform/events"
	"github.com/IncepTools/go/src/web/webhook"
)

// DeliveryReceived is published once per genuinely new delivery.
//
// Publishing rather than calling a handler directly is what lets a second
// subscriber be added later without this file changing.
type DeliveryReceived struct {
	DeliveryID string
	Action     string
}

// WebhookService verifies and dispatches deliveries.
type WebhookService struct {
	secret string
	bus    *events.Bus

	mu   sync.Mutex
	seen map[string]struct{}
}

// NewWebhookService builds the service.
func NewWebhookService(secret string, bus *events.Bus) *WebhookService {
	return &WebhookService{secret: secret, bus: bus, seen: make(map[string]struct{})}
}

// Accept verifies the signature over the RAW body, then parses it.
//
// The order is the whole point. Parsing first and re-encoding to verify
// compares a signature against bytes the sender never signed — key order and
// whitespace both change — so verification must see the exact bytes received.
func (s *WebhookService) Accept(ctx context.Context, body []byte, signature string) (dto.Receipt, error) {
	if err := webhook.Verify(body, s.secret, signature); err != nil {
		// Deliberately vague: a precise message tells a prober whether the
		// signature was malformed or merely wrong, which is a hint worth
		// withholding. webhook.Verify itself compares in constant time.
		return dto.Receipt{}, errs.Unauthorized("signature verification failed").
			WithCode("BAD_SIGNATURE").WithCause(err)
	}

	var d dto.Delivery
	if err := json.Unmarshal(body, &d); err != nil {
		return dto.Receipt{}, errs.Invalid("delivery body is not valid JSON").
			WithCode("MALFORMED_DELIVERY").WithCause(err)
	}
	if d.ID == "" {
		return dto.Receipt{}, errs.Invalid("delivery id is required").WithCode("MISSING_DELIVERY_ID")
	}

	if s.alreadySeen(d.ID) {
		// A sender that receives no 2xx retries, so the same delivery arrives
		// more than once as a matter of course. Treating redelivery as an
		// error is how a webhook endpoint ends up double-charging.
		return dto.Receipt{DeliveryID: d.ID, Duplicate: true}, nil
	}

	if err := events.Publish(ctx, s.bus, DeliveryReceived{DeliveryID: d.ID, Action: d.Action}); err != nil {
		return dto.Receipt{}, err
	}
	return dto.Receipt{DeliveryID: d.ID}, nil
}

// alreadySeen records the delivery id and reports whether it was present.
//
// An in-memory set keeps the example runnable with nothing installed. A real
// service needs this to survive a restart and to be shared across replicas,
// so the set belongs in the database or in Redis with a TTL.
func (s *WebhookService) alreadySeen(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.seen[id]; ok {
		return true
	}
	s.seen[id] = struct{}{}
	return false
}
