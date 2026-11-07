package main

import (
	"context"
	inceptools "github.com/IncepTools/go"
	"github.com/IncepTools/go/examples/queues/apps/queues"
	"github.com/IncepTools/go/src/core/di"
	"github.com/IncepTools/go/src/core/logx"
	"github.com/IncepTools/go/src/platform/queue"
	"github.com/gofiber/fiber/v3"
	"log"
	"log/slog"
	"os"
)

func main() {
	if err := run(); err != nil {
		log.Printf("fatal: %v", err)
		os.Exit(1)
	}
}
func run() error {
	logger := logx.New(logx.Options{Level: slog.LevelDebug, Format: logx.FormatText, Service: "queues"})
	app := inceptools.New(inceptools.Options{Service: "queues", Addr: ":8085", Prefix: "/api/v1", Logger: logger})
	jobs, err := queue.New(queue.Options{Name: "jobs", Workers: 2, MaxAttempts: 3}, func(ctx context.Context, m queue.Message) error {
		logx.C(ctx).Info("job processed", "id", m.ID, "body", string(m.Body))
		return nil
	})
	if err != nil {
		return err
	}
	di.MustSupply(app.Container, jobs)
	app.Lifecycle.Observe("jobs", jobs)
	if err := app.Mount(queues.Descriptor()); err != nil {
		return err
	}
	_ = fiber.StatusAccepted
	return app.Run(context.Background())
}
