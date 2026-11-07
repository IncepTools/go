package inceptools_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gofiber/fiber/v3"

	inceptools "github.com/IncepTools/go"
	"github.com/IncepTools/go/src/core/lifecycle"
)

func TestServeHTTPRunsTheRealPipeline(t *testing.T) {
	app := inceptools.New(inceptools.Options{Service: "test", Prefix: "/api/v1"})
	app.API().Get("/echo/:word", func(c fiber.Ctx) error {
		c.Set("X-Handler", "hit")
		return c.Status(fiber.StatusTeapot).SendString("echo:" + c.Params("word"))
	})

	res := httptest.NewRecorder()
	app.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/v1/echo/hello", nil))

	// Status, headers and body must all survive the adapter: a Lambda
	// deployment has nothing else to return to the gateway.
	if res.Code != fiber.StatusTeapot {
		t.Errorf("status = %d, want %d", res.Code, fiber.StatusTeapot)
	}
	if got := res.Header().Get("X-Handler"); got != "hit" {
		t.Errorf("handler header lost: %q", got)
	}
	if got := res.Body.String(); got != "echo:hello" {
		t.Errorf("body = %q", got)
	}
	// The framework's own middleware ran too, not just the route.
	if res.Header().Get("X-Request-Id") == "" {
		t.Error("the middleware chain did not run through the adapter")
	}
}

func TestPrepareIsSafeToCallOnEveryInvocation(t *testing.T) {
	app := inceptools.New(inceptools.Options{Service: "test"})
	var calls int
	app.Lifecycle.Add(lifecycle.Hook{
		Name:  "cold-start",
		Init:  func(context.Context) error { calls++; return nil },
		Ready: func(context.Context) error { return nil },
	})

	for i := 0; i < 5; i++ {
		if err := app.Prepare(context.Background()); err != nil {
			t.Fatalf("Prepare %d: %v", i, err)
		}
	}
	if calls != 1 {
		t.Fatalf("init ran %d times, want once per cold start", calls)
	}
}

func TestPrepareIsSafeUnderConcurrentInvocations(t *testing.T) {
	app := inceptools.New(inceptools.Options{Service: "test"})
	var mu sync.Mutex
	calls := 0
	app.Lifecycle.Add(lifecycle.Hook{
		Name: "cold-start",
		Init: func(context.Context) error {
			mu.Lock()
			calls++
			mu.Unlock()
			return nil
		},
	})

	// A warm function can be handed several invocations at once; two
	// concurrent cold starts would build every dependency twice.
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := httptest.NewRecorder()
			app.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/livez", nil))
		}()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("init ran %d times under concurrency, want once", calls)
	}
}

func TestPrepareSurvivesACancelledInvocationContext(t *testing.T) {
	app := inceptools.New(inceptools.Options{Service: "test"})
	var initCtx context.Context
	app.Lifecycle.Add(lifecycle.Hook{
		Name: "cold-start",
		Init: func(ctx context.Context) error { initCtx = ctx; return nil },
	})

	// The first invocation's context is cancelled the moment that invocation
	// returns. Boot work tied to it would be cancelled for every invocation
	// after, so Prepare must detach.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := app.Prepare(ctx); err != nil {
		t.Fatalf("Prepare with a cancelled context: %v", err)
	}
	if initCtx == nil {
		t.Fatal("init hook did not run")
	}
	if err := initCtx.Err(); err != nil {
		t.Fatalf("init ran on a cancelled context: %v", err)
	}
}

func TestServeHTTPReportsAFailedPreparationAsUnavailable(t *testing.T) {
	app := inceptools.New(inceptools.Options{Service: "test"})
	boom := errors.New("dependency is down")
	app.Lifecycle.Add(lifecycle.Hook{
		Name: "cold-start",
		Init: func(context.Context) error { return boom },
	})

	res := httptest.NewRecorder()
	app.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/livez", nil))

	// 503, not a panic and not a 200: the gateway must be told this instance
	// cannot serve.
	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", res.Code)
	}
	body, _ := io.ReadAll(res.Body)
	// And it must not leak why.
	if strings.Contains(string(body), "dependency is down") {
		t.Errorf("the boot error leaked to the client: %q", body)
	}
}

func TestReloadReachesModulesThatImplementIt(t *testing.T) {
	app := inceptools.New(inceptools.Options{Service: "test"})
	var first, second int
	app.Lifecycle.AddReload("first", func(context.Context) error { first++; return nil })
	app.Lifecycle.AddReload("second", func(context.Context) error { second++; return nil })

	for i := 0; i < 2; i++ {
		if err := app.Reload(context.Background()); err != nil {
			t.Fatalf("Reload %d: %v", i, err)
		}
	}
	if first != 2 || second != 2 {
		t.Fatalf("first=%d second=%d, want both reloaded twice", first, second)
	}
}

func TestReloadKeepsGoingAfterAFailingCallback(t *testing.T) {
	app := inceptools.New(inceptools.Options{Service: "test"})
	boom := errors.New("bad config file")
	reached := false
	app.Lifecycle.AddReload("broken", func(context.Context) error { return boom })
	app.Lifecycle.AddReload("healthy", func(context.Context) error { reached = true; return nil })

	err := app.Reload(context.Background())
	if err == nil {
		t.Fatal("Reload reported success despite a failing callback")
	}
	if !errors.Is(err, boom) {
		t.Errorf("error does not wrap the cause: %v", err)
	}
	// Independent sections must not be held hostage by one bad one.
	if !reached {
		t.Error("a failing callback stopped the ones after it")
	}
}
