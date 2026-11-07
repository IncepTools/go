package middleware_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/IncepTools/go/src/core/ctxkit"
	"github.com/IncepTools/go/src/web/middleware"
)

func TestRequestIDPropagatesW3CTraceID(t *testing.T) {
	app := fiber.New()
	app.Use(middleware.RequestID())
	app.Get("/", func(c fiber.Ctx) error {
		if got := ctxkit.Trace(c.Context()); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
			t.Fatalf("trace id = %q", got)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	req := httptest.NewRequest(fiber.MethodGet, "/", nil)
	req.Header.Set(middleware.HeaderTraceParent, "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get(middleware.HeaderTraceID); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("X-Trace-Id = %q", got)
	}
}

func TestRequestIDRejectsMalformedTraceParents(t *testing.T) {
	// A traceparent is attacker-controlled on any listener that is not behind
	// your own edge. Anything that does not parse must fall back to a
	// generated id rather than becoming the trace id — otherwise the header
	// is a log-injection vector and a way to collide two unrelated traces.
	cases := []struct {
		name   string
		header string
	}{
		{"empty", ""},
		{"too few fields", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7"},
		{"too many fields", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01-extra"},
		{"short trace id", "00-4bf92f3577b34da6-00f067aa0ba902b7-01"},
		{"short span id", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa-01"},
		{"non-hex trace id", "00-zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz-00f067aa0ba902b7-01"},
		{"all-zero trace id", "00-00000000000000000000000000000000-00f067aa0ba902b7-01"},
		{"all-zero span id", "00-4bf92f3577b34da6a3ce929d0e0e4736-0000000000000000-01"},
		{"log injection attempt", "00-4bf92f3577b34da6a3ce929d0e0e4736\n-00f067aa0ba902b7-01"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New()
			app.Use(middleware.RequestID())
			app.Get("/", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusNoContent) })

			req := httptest.NewRequest(fiber.MethodGet, "/", nil)
			if tc.header != "" {
				req.Header.Set(middleware.HeaderTraceParent, tc.header)
			}
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			traceID := resp.Header.Get(middleware.HeaderTraceID)
			requestID := resp.Header.Get(middleware.HeaderRequestID)
			if traceID == "" || requestID == "" {
				t.Fatalf("missing correlation headers: trace=%q request=%q", traceID, requestID)
			}
			// Rejected: the trace id is the locally generated request id.
			if traceID != requestID {
				t.Fatalf("malformed traceparent was adopted: trace=%q request=%q", traceID, requestID)
			}
		})
	}
}

func TestRequestIDKeepsTheRequestIDSeparateFromTheTraceID(t *testing.T) {
	app := fiber.New()
	app.Use(middleware.RequestID())
	app.Get("/", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusNoContent) })

	req := httptest.NewRequest(fiber.MethodGet, "/", nil)
	req.Header.Set(middleware.HeaderRequestID, "caller-supplied-request-id")
	req.Header.Set(middleware.HeaderTraceParent, "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// The trace identifies the call across services; the request id
	// identifies this hop. Collapsing them makes a retried request
	// indistinguishable from the original.
	if got := resp.Header.Get(middleware.HeaderRequestID); got != "caller-supplied-request-id" {
		t.Errorf("X-Request-Id = %q", got)
	}
	if got := resp.Header.Get(middleware.HeaderTraceID); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("X-Trace-Id = %q", got)
	}
}

func TestRequestIDReplacesAnAbsurdlyLongRequestID(t *testing.T) {
	app := fiber.New()
	app.Use(middleware.RequestID())
	app.Get("/", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusNoContent) })

	req := httptest.NewRequest(fiber.MethodGet, "/", nil)
	req.Header.Set(middleware.HeaderRequestID, strings.Repeat("a", 201))
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get(middleware.HeaderRequestID); len(got) > 200 {
		t.Fatalf("a 201-character request id was echoed back (%d chars)", len(got))
	}
}
