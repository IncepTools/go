package main

import (
	"context"
	inceptools "github.com/IncepTools/go"
	"github.com/IncepTools/go/examples/scheduling/apps/jobs"
	"github.com/IncepTools/go/src/core/di"
	"github.com/IncepTools/go/src/core/logx"
	"github.com/IncepTools/go/src/platform/lock"
	"github.com/IncepTools/go/src/platform/scheduler"
	"log"
	"log/slog"
	"os"
	"time"
)

func main() {
	if err := run(); err != nil {
		log.Printf("fatal: %v", err)
		os.Exit(1)
	}
}
func run() error {
	logger := logx.New(logx.Options{Level: slog.LevelDebug, Format: logx.FormatText, Service: "scheduling"})
	app := inceptools.New(inceptools.Options{Service: "scheduling", Addr: ":8086", Logger: logger})
	sched := scheduler.New(lock.NewMemory())
	if err := sched.Add(scheduler.Job{Name: "reports.daily", Every: 5 * time.Second, RunOnStart: true, Singleton: true, Run: func(ctx context.Context) error { logx.C(ctx).Info("daily report built"); return nil }}); err != nil {
		return err
	}
	di.MustSupply(app.Container, sched)
	app.Lifecycle.Observe("scheduler", sched)
	if err := app.Mount(jobs.Descriptor()); err != nil {
		return err
	}
	return app.Run(context.Background())
}
