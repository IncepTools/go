package middleware_test

import (
	"net/http/httptest"
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
