package main

import (
	"context"
	inceptools "github.com/IncepTools/go"
	"github.com/IncepTools/go/examples/sse/apps/stream"
	"github.com/IncepTools/go/src/core/logx"
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
	logger := logx.New(logx.Options{Level: slog.LevelDebug, Format: logx.FormatText, Service: "sse"})
	app := inceptools.New(inceptools.Options{Service: "sse", Addr: ":8087", Prefix: "/api/v1", Logger: logger})
	if err := app.Mount(stream.Descriptor()); err != nil {
		return err
	}
	return app.Run(context.Background())
}
