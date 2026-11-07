package main

import (
	"context"
	inceptools "github.com/IncepTools/go"
	eventsapp "github.com/IncepTools/go/examples/event-emitter/apps/events"
	"github.com/IncepTools/go/src/core/di"
	"github.com/IncepTools/go/src/core/logx"
	"github.com/IncepTools/go/src/platform/events"
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
	logger := logx.New(logx.Options{Level: slog.LevelDebug, Format: logx.FormatText, Service: "event-emitter"})
	bus := events.New()
	app := inceptools.New(inceptools.Options{Service: "event-emitter", Addr: ":8088", Prefix: "/api/v1", Logger: logger})
	di.MustSupply(app.Container, bus)
	if err := app.Mount(eventsapp.Descriptor()); err != nil {
		return err
	}
	return app.Run(context.Background())
}
