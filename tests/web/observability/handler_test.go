package observability_test

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/IncepTools/go/src/web/observability"
)

func TestHandlerServesPrometheusTextExposition(t *testing.T) {
	m := observability.New()
	app := fiber.New()
	app.Use(m.Middleware())
	app.Get("/metrics", m.Handler)
	app.Get("/ping", func(c fiber.Ctx) error { return c.SendString("pong") })

	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/ping", nil))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	resp, err = app.Test(httptest.NewRequest(fiber.MethodGet, "/metrics", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// The content type is what makes Prometheus parse it rather than store it
	// as an opaque blob.
	if got := resp.Header.Get(fiber.HeaderContentType); !strings.HasPrefix(got, "text/plain") {
		t.Errorf("Content-Type = %q", got)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, want := range []string{
		"# HELP inceptools_http_requests_total",
		"# TYPE inceptools_http_requests_total counter",
		`inceptools_http_requests_total{method="GET",route="/ping",status_class="2xx"} 1`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("exposition missing %q:\n%s", want, text)
		}
	}
}

func TestMetricsCountsHandlerFailuresAsWellAsSuccesses(t *testing.T) {
	m := observability.New()
	app := fiber.New()
	app.Use(m.Middleware())
	app.Get("/boom", func(fiber.Ctx) error { return fiber.ErrInternalServerError })

	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/boom", nil))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// Recording only on the success path is how a route that fails every
	// time shows up as no traffic at all.
	if got := m.Prometheus(); !strings.Contains(got, `route="/boom",status_class="5xx"`) {
		t.Errorf("failure was not counted:\n%s", got)
	}
}

func TestMetricsDoesNotCreateASeriesPerUnmatchedURL(t *testing.T) {
	m := observability.New()
	app := fiber.New()
	app.Use(m.Middleware())
	app.Get("/known", func(c fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })

	// A scanner probing random paths must not be able to grow the metric
	// cardinality without bound.
	for _, path := range []string{"/wp-admin", "/.env", "/phpmyadmin"} {
		resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, path, nil))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}

	text := m.Prometheus()
	for _, probe := range []string{"/wp-admin", "/.env", "/phpmyadmin"} {
		if strings.Contains(text, `route="`+probe+`"`) {
			t.Errorf("unmatched path %q became its own series:\n%s", probe, text)
		}
	}
	if got := strings.Count(text, "inceptools_http_requests_total{"); got > 2 {
		t.Errorf("three probes produced %d series, want them collapsed:\n%s", got, text)
	}
}
