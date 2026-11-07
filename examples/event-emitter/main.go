// Command event-emitter publishes a typed event and fans it out.
//
//	go run ./event-emitter
//	curl -X POST localhost:8088/api/v1/events/notifications/orders \
//	  -H 'Content-Type: application/json' \
//	  -d '{"reference":"A-1","email":"buyer@example.com","total_cents":2500}'
//
// Watch the order of the logs: stock is reserved before the response, the
// confirmation and the audit arrive after it. Ask for more than 1,000,000
// cents to see a SYNC subscriber refuse the order outright.
package main

import (
	"context"
	"log"
	"log/slog"
	"os"

	inceptools "github.com/IncepTools/go"
	"github.com/IncepTools/go/examples/event-emitter/apps/events"
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
		Service: "event-emitter",
	})

	app := inceptools.New(inceptools.Options{
		Service: "event-emitter",
		Addr:    ":8088",
		Prefix:  "/api/v1",
		Logger:  logger,
		Middleware: middleware.ChainOptions{
			AccessLog: middleware.AccessLogOptions{
				SkipPaths: []string{"/livez", "/healthz", "/readyz"},
			},
		},
	})

	if err := app.Mount(events.Descriptor()); err != nil {
		return err
	}
	return app.Run(context.Background())
}
