// Command queues accepts work over HTTP and processes it in the background.
//
//	go run ./queues
//	curl -X POST localhost:8085/api/v1/queues/jobs \
//	  -H 'Content-Type: application/json' \
//	  -d '{"kind":"report","subject":"october"}'
//
// Ask for a failure to watch the retry and dead-letter paths:
//
//	curl -X POST localhost:8085/api/v1/queues/jobs \
//	  -H 'Content-Type: application/json' \
//	  -d '{"kind":"export","subject":"doomed","fail":true}'
package main

import (
	"context"
	"log"
	"log/slog"
	"os"

	inceptools "github.com/IncepTools/go"
	"github.com/IncepTools/go/examples/queues/apps/queues"
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
		Service: "queues",
	})

	app := inceptools.New(inceptools.Options{
		Service: "queues",
		Addr:    ":8085",
		Prefix:  "/api/v1",
		Logger:  logger,
		Middleware: middleware.ChainOptions{
			AccessLog: middleware.AccessLogOptions{
				SkipPaths: []string{"/livez", "/healthz", "/readyz"},
			},
		},
	})

	if err := app.Mount(queues.Descriptor()); err != nil {
		return err
	}
	return app.Run(context.Background())
}
