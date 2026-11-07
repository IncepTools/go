// Command webhooks verifies signed inbound deliveries.
//
//	go run ./webhooks
//
//	BODY='{"id":"d-1","action":"created"}'
//	SIG=$(printf '%s' "$BODY" | openssl dgst -sha256 -hmac development-secret -r | cut -d' ' -f1)
//	curl -X POST localhost:8084/api/v1/webhooks/incoming \
//	  -H "X-Hub-Signature-256: sha256=$SIG" -d "$BODY"
//
// Send it twice: the second response reports duplicate=true rather than
// dispatching the event again.
package main

import (
	"context"
	"log"
	"log/slog"
	"os"

	inceptools "github.com/IncepTools/go"
	"github.com/IncepTools/go/examples/webhooks/apps/webhooks"
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
		Service: "webhooks",
	})

	app := inceptools.New(inceptools.Options{
		Service: "webhooks",
		Addr:    ":8084",
		Prefix:  "/api/v1",
		Logger:  logger,
		Middleware: middleware.ChainOptions{
			AccessLog: middleware.AccessLogOptions{
				SkipPaths: []string{"/livez", "/healthz", "/readyz"},
			},
		},
	})

	if err := app.Mount(webhooks.Descriptor()); err != nil {
		return err
	}
	return app.Run(context.Background())
}
