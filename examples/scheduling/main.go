// Command scheduling runs singleton scheduled jobs and drains them cleanly.
//
//	go run ./scheduling
//	curl localhost:8086/api/v1/jobs/reports
//	curl localhost:8086/api/v1/jobs/reports/reports.daily
//
// Then stop it with Ctrl-C and watch the run in flight finish before the
// process exits.
//
// Note what is NOT here: no scheduler, no jobs, no lifecycle registration.
// The jobs domain owns its engine and the reports module declares its own
// schedule, so mounting the domain is the whole wiring.
package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"time"

	inceptools "github.com/IncepTools/go"
	"github.com/IncepTools/go/examples/scheduling/apps/jobs"
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
		Service: "scheduling",
	})

	app := inceptools.New(inceptools.Options{
		Service: "scheduling",
		Addr:    ":8086",
		Prefix:  "/api/v1",
		Logger:  logger,
		// Give a job in flight room to finish. The scheduler's shutdown waits
		// for it, and this is the budget that wait is bounded by.
		DrainTimeout: 30 * time.Second,
		Middleware: middleware.ChainOptions{
			AccessLog: middleware.AccessLogOptions{
				SkipPaths: []string{"/livez", "/healthz", "/readyz"},
			},
		},
	})

	if err := app.Mount(jobs.Descriptor()); err != nil {
		return err
	}
	return app.Run(context.Background())
}
