// Command serverless hosts one inceptools application behind a net/http
// server instead of Run:
//
//	go run ./serverless
//	curl localhost:8089/api/v1/catalog/quotes/acme
//	curl localhost:8089/livez
//
// Change the configuration and ask the host to reload it, with no restart:
//
//	echo '{"currency":"EUR"}' > quotes.json
//	curl -X POST localhost:8089/__control/reload
//	curl localhost:8089/api/v1/catalog/quotes/acme   # now priced in EUR
//
// The point is that nothing about the application changes between hosting
// modes. The same domain, middleware, health routes and typed errors run
// under Run, under this net/http host, and under a Lambda adapter — because
// Application implements http.Handler. net/http is used here so the example
// stays runnable with no AWS dependency and no credentials; see the Lambda
// note below for the two lines that differ.
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
//     refreshing configuration has to be an explicit call. It is the HOST's
//     call, not a route: the quotes module implements OnReload and knows
//     nothing about how the refresh was triggered.
package main

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"

	inceptools "github.com/IncepTools/go"
	"github.com/IncepTools/go/examples/serverless/apps/catalog"
	"github.com/IncepTools/go/src/core/logx"
	"github.com/IncepTools/go/src/web/middleware"
)

func main() {
	if err := run(); err != nil {
		log.Printf("fatal: %v", err)
		os.Exit(1)
	}
}

func run() error {
	logger := logx.New(logx.Options{
		Level:   slog.LevelDebug,
		Format:  logx.FormatText,
		Service: "serverless",
	})

	// Cold start: build the application once and keep it for the life of the
	// execution environment. A handler that builds it per invocation pays the
	// wiring cost every time and loses every pooled client it opened.
	app := inceptools.New(inceptools.Options{
		Service: "serverless",
		Prefix:  "/api/v1",
		Logger:  logger,
		Middleware: middleware.ChainOptions{
			Timeout: 10 * time.Second,
			AccessLog: middleware.AccessLogOptions{
				SkipPaths: []string{"/livez", "/healthz", "/readyz"},
			},
		},
		// Telemetry: tel — one telemetry.Telemetry built here, during the
		// cold start, is reused by every invocation. See docs/telemetry.md.
	})

	if err := app.Mount(catalog.Descriptor()); err != nil {
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
			logger.Error("shutdown failed", "error", err)
		}
	}()

	// In Lambda the application is handed to the adapter instead, unchanged:
	//
	//	adapter := httpadapter.NewV2(app)
	//	lambda.Start(adapter.ProxyWithContext)
	//
	// Here it is net/http, which takes the same http.Handler.
	srv := &http.Server{
		Addr:              ":8089",
		Handler:           controlPlane(app, logger),
		ReadHeaderTimeout: 5 * time.Second,
	}
	logger.Info("serving the application as an http.Handler", "addr", ":8089")
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// controlPlane stands in for whatever triggers a reload in a real deployment:
// a Lambda extension, a parameter-store subscription, a deploy hook. It is
// the host's surface, kept out of the application's own routes so a
// configuration refresh is not something any caller can trigger.
func controlPlane(app *inceptools.Application, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /__control/reload", func(w http.ResponseWriter, r *http.Request) {
		// Every module that implements OnReload refreshes itself. One
		// failure does not stop the others, and a module that fails keeps
		// serving its previous configuration.
		if err := app.Reload(r.Context()); err != nil {
			logger.Error("reload failed", "error", err)
			http.Error(w, "reload failed", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.Handle("/", app)
	return mux
}
