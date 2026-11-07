// Command worker is an inceptools service with no API.
//
// It serves only the health endpoints; everything else it does happens on a
// schedule. A worker is still a service — it has modules, dependencies,
// lifecycle and health — and this example exists because that is easy to
// miss when every other example is a REST API.
//
//	go run ./worker
//	curl localhost:8082/livez
//	curl localhost:8082/health
//
// Press Ctrl-C and watch it drain: the scheduler stops accepting
// occurrences, the run in flight finishes under the drain deadline, and the
// module's OnShutdown reports what it did.
package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"time"

	inceptools "github.com/IncepTools/go"
	"github.com/IncepTools/go/examples/worker/apps/jobs"
	"github.com/IncepTools/go/src/core/di"
	"github.com/IncepTools/go/src/core/logx"
	"github.com/IncepTools/go/src/platform/lock"
	"github.com/IncepTools/go/src/platform/scheduler"
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
		Level: slog.LevelDebug, Format: logx.FormatText, Service: "worker",
	})
	slog.SetDefault(logger)

	app := inceptools.New(inceptools.Options{
		Service: "worker",
		Version: "1.0.0",
		Addr:    ":8082",
		Prefix:  "/api/v1",
		Logger:  logger,
		// 40 seconds, so a run in flight can finish. Keep this below the
		// grace period your orchestrator gives, or the process is killed
		// mid-drain.
		DrainTimeout: 40 * time.Second,
		Middleware: middleware.ChainOptions{
			AccessLog: middleware.AccessLogOptions{SkipPaths: []string{"/livez", "/healthz", "/readyz"}},
		},
	})

	// An in-memory locker, because this example is one process. Swap in
	// lock.NewRedis for several replicas and the Singleton jobs then run on
	// exactly one of them — nothing else changes.
	sched := scheduler.New(lock.NewMemory())
	di.MustSupply(app.Container, sched)

	// Registering the scheduler with the lifecycle is what starts it in
	// OnReady and drains it on the way down.
	app.Lifecycle.Observe("scheduler", sched)

	if err := app.Mount(jobs.Descriptor()); err != nil {
		return err
	}

	logger.Info("scheduled jobs", "names", sched.Jobs())
	return app.Run(context.Background())
}
