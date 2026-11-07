package observability_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/IncepTools/go/src/web/observability"
)

func TestMetricsUsesRouteTemplateAndStatusClass(t *testing.T) {
	m := observability.New()
	app := fiber.New()
	app.Use(m.Middleware())
	app.Get("/users/:id", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusCreated) })

	for _, id := range []string{"a", "b"} {
		req := httptest.NewRequest(fiber.MethodGet, "/users/"+id, nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}

	text := m.Prometheus()
	for _, want := range []string{`route="/users/:id"`, `status_class="2xx"`, "inceptools_http_requests_total"} {
		if !strings.Contains(text, want) {
			t.Errorf("metrics missing %q:\n%s", want, text)
		}
	}
	for _, unwanted := range []string{`route="/users/a"`, `route="/users/b"`} {
		if strings.Contains(text, unwanted) {
			t.Errorf("metrics contained unbounded route %q:\n%s", unwanted, text)
		}
	}
	if got := strings.Count(text, `method="GET",route="/users/:id",status_class="2xx"`); got != 2 {
		t.Errorf("expected two metric samples with the route labels, got %d", got)
	}
}
