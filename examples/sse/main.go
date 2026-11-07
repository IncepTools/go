// Command sse streams server-sent events.
//
//	go run ./sse
//	curl -N localhost:8087/api/v1/stream/events
//	curl -N 'localhost:8087/api/v1/stream/events?interval=500ms'
//	curl -N -H 'Last-Event-ID: 40' localhost:8087/api/v1/stream/events
//
// -N matters: without it curl buffers and the stream looks frozen.
package main

import (
	"context"
	"log"
	"log/slog"
	"os"

	inceptools "github.com/IncepTools/go"
	"github.com/IncepTools/go/examples/sse/apps/stream"
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
		Service: "sse",
	})

	app := inceptools.New(inceptools.Options{
		Service: "sse",
		Addr:    ":8087",
		Prefix:  "/api/v1",
		Logger:  logger,
		Middleware: middleware.ChainOptions{
			// No request Timeout: the chain's timeout would cancel a stream
			// that is behaving exactly as intended. A long-lived response
			// needs its own idle policy, which the SSE heartbeat provides.
			AccessLog: middleware.AccessLogOptions{
				SkipPaths: []string{"/livez", "/healthz", "/readyz"},
			},
		},
	})

	if err := app.Mount(stream.Descriptor()); err != nil {
		return err
	}
	return app.Run(context.Background())
}
