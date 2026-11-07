package main

import (
	"context"
	"log"
	"log/slog"
	"os"

	inceptools "github.com/IncepTools/go"
	"github.com/IncepTools/go/examples/webhooks/apps/webhooks"
	"github.com/IncepTools/go/src/core/di"
	"github.com/IncepTools/go/src/core/logx"
	"github.com/IncepTools/go/src/platform/events"
)

func main() {
	if err := run(); err != nil {
		log.Printf("fatal: %v", err)
		os.Exit(1)
	}
}
func run() error {
	logger := logx.New(logx.Options{Level: slog.LevelDebug, Format: logx.FormatText, Service: "webhooks"})
	app := inceptools.New(inceptools.Options{Service: "webhooks", Addr: ":8084", Prefix: "/api/v1", Logger: logger})
	di.MustSupply(app.Container, events.New())
	if err := app.Mount(webhooks.Descriptor()); err != nil {
		return err
	}
	return app.Run(context.Background())
}
