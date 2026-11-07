package inceptools_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	inceptools "github.com/IncepTools/go"
	"github.com/IncepTools/go/src/core/lifecycle"
)

func TestFactoryMountsRivaasStyleProbeAliases(t *testing.T) {
	app := inceptools.New(inceptools.Options{Service: "test"})
	for _, path := range []string{"/livez", "/healthz", "/readyz"} {
		resp, err := app.Fiber.Test(httptest.NewRequest("GET", path, nil))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("%s returned %d", path, resp.StatusCode)
		}
	}
}

func TestApplicationReloadCanBeCalledWithoutSignals(t *testing.T) {
	app := inceptools.New(inceptools.Options{Service: "test"})
	called := false
	app.OnReload(func(context.Context) error {
		called = true
		return nil
	})
	if err := app.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("reload callback was not called")
	}
}

func TestServeHTTPPreparesOnceForServerless(t *testing.T) {
	app := inceptools.New(inceptools.Options{Service: "test"})
	initCalls, readyCalls := 0, 0
	app.Lifecycle.Add(lifecycle.Hook{
		Name:  "cold-start",
		Init:  func(context.Context) error { initCalls++; return nil },
		Ready: func(context.Context) error { readyCalls++; return nil },
	})
	for i := 0; i < 2; i++ {
		res := httptest.NewRecorder()
		app.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/livez", nil))
		if res.Code != http.StatusOK {
			t.Fatalf("request %d returned %d", i, res.Code)
		}
	}
	if initCalls != 1 || readyCalls != 1 {
		t.Fatalf("init=%d ready=%d, want one cold-start preparation", initCalls, readyCalls)
	}
}
