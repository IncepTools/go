// Command serverless hosts one inceptools application behind a net/http
// server instead of Run:
//
//	go run ./serverless
//	curl localhost:8089/api/v1/quotes/quotes/acme
//	curl localhost:8089/livez
//	curl -X POST localhost:8089/api/v1/quotes/quotes/reload
//
// The point is that nothing about the application changes between hosting
// modes. The same descriptor, middleware, health routes and typed errors run
// under Run, under this net/http host, and under a Lambda adapter — because
// Application implements http.Handler. net/http is used here so the example
// stays runnable with no AWS dependency and no credentials; see the Lambda
// note at the bottom of this file for the four lines that differ.
//
// Two things a serverless host must do that a server does not:
//
//   - Prepare instead of Run. Run owns a listener and a signal loop, neither
//     of which exists in a function runtime. Prepare does the boot work —
//     eager wiring, init hooks, ready hooks — and is safe to call on every
//     invocation, because it runs once per cold start. ServeHTTP calls it, so
//     calling it during the cold start is an optimisation, not a requirement:
//     it moves the wiring cost off the first request.
//
//   - Reload instead of SIGHUP. A function runtime delivers no signals, so
//     configuration refresh has to be an explicit call.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/gofiber/fiber/v3"

	inceptools "github.com/IncepTools/go"
	"github.com/IncepTools/go/src/core/di"
	"github.com/IncepTools/go/src/core/errs"
	"github.com/IncepTools/go/src/core/module"
	"github.com/IncepTools/go/src/web/middleware"
	"github.com/IncepTools/go/src/web/pipe"
	"github.com/IncepTools/go/src/web/response"
	"github.com/IncepTools/go/src/web/router"
)

func main() {
	if err := run(); err != nil {
		log.Printf("fatal: %v", err)
		os.Exit(1)
	}
}

func run() error {
	// Cold start: build the application once and keep it for the life of the
	// execution environment. A handler that builds it per invocation pays the
	// wiring cost every time and loses every pooled client it opened.
	app := inceptools.New(inceptools.Options{
		Service: "serverless",
		Prefix:  "/api/v1",
		Middleware: middleware.ChainOptions{
			Timeout: 10 * time.Second,
			AccessLog: middleware.AccessLogOptions{
				SkipPaths: []string{"/livez", "/healthz", "/readyz"},
			},
		},
		// Telemetry: tel — one telemetry.Telemetry built here, during the cold
		// start, is reused by every invocation. See docs/telemetry.md.
	})

	cfg := &settings{}
	cfg.currency.Store("USD")

	// A server receives this through SIGHUP. A function runtime has no
	// signals, so the same callback is reached through app.Reload instead.
	app.OnReload(func(context.Context) error { return cfg.reload() })

	if err := app.Mount(quotesDescriptor(app, cfg)); err != nil {
		return err
	}

	// Prepare is what Run would have done before listening. Doing it here
	// rather than on the first request keeps the first invocation off the
	// wiring path.
	ctx := context.Background()
	if err := app.Prepare(ctx); err != nil {
		return err
	}
	defer func() {
		// Only when the environment is genuinely terminating — never after an
		// invocation. Shutdown flushes telemetry and closes pooled clients.
		if err := app.Shutdown(context.Background()); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}()

	// In Lambda this is the adapter instead, over the same app value:
	//
	//	adapter := httpadapter.NewV2(app)
	//	lambda.Start(adapter.ProxyWithContext)
	//
	// Here it is net/http, which takes the same http.Handler.
	srv := &http.Server{
		Addr:              ":8089",
		Handler:           app,
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Println("serving the application as an http.Handler on :8089")
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// --- configuration refreshed without a restart ------------------------

type settings struct {
	currency atomic.Value
	reloads  atomic.Int64
}

// reload stands in for re-reading a parameter store or secrets manager. It
// must be safe to call while requests are in flight, which is why the value
// is swapped atomically rather than mutated in place.
func (s *settings) reload() error {
	next := "USD"
	if s.reloads.Load()%2 == 0 {
		next = "EUR"
	}
	s.currency.Store(next)
	s.reloads.Add(1)
	return nil
}

// --- the domain -------------------------------------------------------

func quotesDescriptor(app *inceptools.Application, cfg *settings) module.Descriptor {
	return module.Descriptor{
		Build: func(*di.Scope) module.App {
			return module.NewApp("quotes", &quotesModule{
				Base: module.NewBase("quotes", module.WithControllers(
					router.ControllerFunc(func() []router.Route {
						return []router.Route{
							router.Post("/reload", reload(app)).
								Summary("Refresh configuration without a restart").
								AsPublic(),
							router.Get("/:symbol", quote(cfg)).
								Summary("Quote one symbol").
								Param("symbol", "The symbol to quote").
								AsPublic(),
						}
					}),
				)),
			})
		},
	}
}

type quotesModule struct{ module.Base }

func quote(cfg *settings) fiber.Handler {
	return func(c fiber.Ctx) error {
		symbol, err := pipe.StringParam(c, "symbol")
		if err != nil {
			return response.Fail(c, err)
		}
		if symbol == "unknown" {
			return response.Fail(c, errs.NotFound("no quote for %q", symbol).
				WithCode("NO_SUCH_SYMBOL"))
		}
		return response.OK(c, "quote rendered", fiber.Map{
			"symbol":   symbol,
			"currency": cfg.currency.Load(),
		})
	}
}

// reload is the serverless equivalent of SIGHUP. A real function would call
// app.Reload from its control-plane lifecycle rather than exposing it as a
// route; it is a route here so the example is one curl away.
func reload(app *inceptools.Application) fiber.Handler {
	return func(c fiber.Ctx) error {
		if err := app.Reload(c.Context()); err != nil {
			return response.Fail(c, err)
		}
		return response.OK(c, "configuration reloaded", nil)
	}
}
