package response_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/IncepTools/go/src/core/errs"
	"github.com/IncepTools/go/src/web/response"
)

func TestStatusForErrorMatchesWhatTheFilterWillWrite(t *testing.T) {
	// Instrumentation uses this to label a request whose status has not been
	// written yet. If it disagrees with the exception filter, dashboards
	// disagree with the responses clients actually received.
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"no error", nil, fiber.StatusOK},
		{"invalid", errs.Invalid("bad field"), fiber.StatusBadRequest},
		{"unauthorized", errs.Unauthorized("no token"), fiber.StatusUnauthorized},
		{"forbidden", errs.Forbidden("not yours"), fiber.StatusForbidden},
		{"not found", errs.NotFound("no such order"), fiber.StatusNotFound},
		{"conflict", errs.Conflict("already exists"), fiber.StatusConflict},
		{"exhausted", errs.Exhausted("slow down"), fiber.StatusTooManyRequests},
		{"payment required", errs.PaymentRequired("card declined"), fiber.StatusPaymentRequired},
		{"unavailable", errs.Unavailable("upstream down"), fiber.StatusServiceUnavailable},
		{"timeout", errs.Timeout("took too long"), fiber.StatusGatewayTimeout},
		{"not supported", errs.NotSupported("not implemented"), fiber.StatusNotImplemented},
		{"internal", errs.Internal("boom"), fiber.StatusInternalServerError},
		{"a fiber error keeps its code", fiber.ErrTeapot, fiber.StatusTeapot},
		// An unclassified error is a 500 by definition: nobody decided what
		// it means, so it cannot be anything milder.
		{"unclassified", errors.New("something went wrong"), fiber.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := response.StatusForError(tc.err); got != tc.want {
				t.Fatalf("StatusForError(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

func TestStatusForErrorSeesThroughWrapping(t *testing.T) {
	// Errors arrive wrapped by the layers they passed through. A mapping
	// that only matches the top-level error degrades every one of them to a
	// 500 the moment somebody adds context.
	wrapped := fmt.Errorf("loading the order: %w", errs.NotFound("no such order"))
	if got := response.StatusForError(wrapped); got != fiber.StatusNotFound {
		t.Fatalf("wrapped not-found = %d, want 404", got)
	}

	doubly := fmt.Errorf("handling the request: %w", wrapped)
	if got := response.StatusForError(doubly); got != fiber.StatusNotFound {
		t.Fatalf("doubly wrapped not-found = %d, want 404", got)
	}
}
