package idempotency_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/IncepTools/go/src/reliability/idempotency"
)

func newApp(executions *atomic.Int32, status int) *fiber.App {
	app := fiber.New()
	app.Use(idempotency.New(idempotency.Options{
		Store: idempotency.NewMemoryStore(),
		TTL:   time.Minute,
	}))
	app.Post("/vms", func(c fiber.Ctx) error {
		executions.Add(1)
		return c.Status(status).JSON(fiber.Map{"id": "vm-1"})
	})
	return app
}

func post(t *testing.T, app *fiber.App, key, body string) *http.Response {
	t.Helper()
	req := httptest.NewRequest("POST", "/vms", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set(idempotency.HeaderKey, key)
	}
	res, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestRetryWithTheSameKeyDoesNotExecuteTwice(t *testing.T) {
	var executions atomic.Int32
	app := newApp(&executions, http.StatusCreated)

	first := post(t, app, "key-1", `{"size":"small"}`)
	second := post(t, app, "key-1", `{"size":"small"}`)

	if executions.Load() != 1 {
		t.Fatalf("handler ran %d times; a retried POST must not provision twice", executions.Load())
	}
	if first.StatusCode != second.StatusCode {
		t.Fatalf("status %d then %d; the replay must reproduce the original", first.StatusCode, second.StatusCode)
	}
	if second.Header.Get(idempotency.HeaderReplayed) != "true" {
		t.Fatal("the replay was not marked; a client cannot tell it from a fresh execution")
	}
}

func TestDifferentKeysExecuteSeparately(t *testing.T) {
	var executions atomic.Int32
	app := newApp(&executions, http.StatusCreated)

	post(t, app, "key-1", `{}`)
	post(t, app, "key-2", `{}`)

	if executions.Load() != 2 {
		t.Fatalf("executions = %d, want 2", executions.Load())
	}
}

func TestReusingAKeyWithADifferentBodyIsRefused(t *testing.T) {
	var executions atomic.Int32
	app := newApp(&executions, http.StatusCreated)

	post(t, app, "key-1", `{"size":"small"}`)
	res := post(t, app, "key-1", `{"size":"enormous"}`)

	if res.StatusCode == http.StatusCreated {
		t.Fatal("a reused key with a changed body replayed the old response, hiding a client bug")
	}
	if executions.Load() != 1 {
		t.Fatalf("executions = %d, want 1", executions.Load())
	}
}

func TestNoKeyMeansNoProtection(t *testing.T) {
	var executions atomic.Int32
	app := newApp(&executions, http.StatusCreated)

	post(t, app, "", `{}`)
	post(t, app, "", `{}`)

	if executions.Load() != 2 {
		t.Fatalf("executions = %d; without Require, a missing key must pass through", executions.Load())
	}
}

func TestServerErrorsAreNotRecorded(t *testing.T) {
	// Recording a 500 would pin a transient failure to the key for the whole
	// TTL, so the client could never succeed with it.
	var executions atomic.Int32
	app := newApp(&executions, http.StatusInternalServerError)

	post(t, app, "key-1", `{}`)
	post(t, app, "key-1", `{}`)

	if executions.Load() != 2 {
		t.Fatalf("executions = %d; a 5xx must be retryable with the same key", executions.Load())
	}
}

func TestGetIsNotIntercepted(t *testing.T) {
	var executions atomic.Int32
	app := fiber.New()
	app.Use(idempotency.New(idempotency.Options{Store: idempotency.NewMemoryStore()}))
	app.Get("/vms", func(c fiber.Ctx) error {
		executions.Add(1)
		return c.SendString("ok")
	})

	for range 2 {
		req := httptest.NewRequest("GET", "/vms", nil)
		req.Header.Set(idempotency.HeaderKey, "key-1")
		if _, err := app.Test(req); err != nil {
			t.Fatal(err)
		}
	}
	if executions.Load() != 2 {
		t.Fatalf("executions = %d; a GET changes nothing and must not be replayed", executions.Load())
	}
}

func TestRequireRejectsAMissingKey(t *testing.T) {
	app := fiber.New()
	app.Use(idempotency.New(idempotency.Options{Store: idempotency.NewMemoryStore(), Require: true}))
	app.Post("/vms", func(c fiber.Ctx) error { return c.SendStatus(201) })

	res := post(t, app, "", `{}`)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", res.StatusCode)
	}
}
