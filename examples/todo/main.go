// Command todo is a runnable inceptools service.
//
// It keeps everything in memory, so it needs no database:
//
//	go run ./examples/todo
//
//	curl localhost:8080/livez
//	curl localhost:8080/readyz
//
//	ACC=11111111-1111-1111-1111-111111111111
//	USR=22222222-2222-2222-2222-222222222222
//
//	curl -X POST localhost:8080/api/v1/todo/tasks \
//	     -H 'Content-Type: application/json' \
//	     -H "X-Account-Id: $ACC" -H "X-User-Id: $USR" \
//	     -d '{"title":"Write the docs","priority":"high","tags":["Writing"]}'
//
//	curl "localhost:8080/api/v1/todo/tasks?sort=title" \
//	     -H "X-Account-Id: $ACC" -H "X-User-Id: $USR"
package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	inceptools "github.com/IncepTools/go"
	"github.com/IncepTools/go/examples/todo/apps/todo"
	"github.com/IncepTools/go/src/core/ctxkit"
	"github.com/IncepTools/go/src/core/di"
	"github.com/IncepTools/go/src/core/logx"
	"github.com/IncepTools/go/src/platform/cache"
	"github.com/IncepTools/go/src/platform/events"
	"github.com/IncepTools/go/src/platform/lock"
	"github.com/IncepTools/go/src/platform/scheduler"
	"github.com/IncepTools/go/src/web/middleware"
)

func main() {
	if err := run(); err != nil {
		// Log and exit non-zero. A service that cannot start must not come
		// up half-working and begin serving errors.
		log.Printf("fatal: %v", err)
		os.Exit(1)
	}
}

func run() error {
	logger := logx.New(logx.Options{
		Level: slog.LevelDebug, Format: logx.FormatText, Service: "todo",
	})
	slog.SetDefault(logger)

	app := inceptools.New(inceptools.Options{
		Service: "todo",
		Version: "1.0.0",
		Addr:    ":8080",
		Prefix:  "/api/v1",
		Logger:  logger,
		Middleware: middleware.ChainOptions{
			Timeout:  15 * time.Second,
			Identity: headerIdentity,
			AccessLog: middleware.AccessLogOptions{
				LogSuccess:    true,
				SkipPaths:     []string{"/livez", "/healthz", "/readyz"},
				SlowThreshold: 500 * time.Millisecond,
			},
			SecureHeaders: &middleware.SecureHeadersOptions{},
		},
		CORS: &middleware.CORSOptions{AllowOrigins: []string{"*"}},
	})

	// Dependencies built outside the container are supplied into it. The
	// in-memory implementations here are the single-instance ones; a real
	// deployment swaps in the Redis-backed cache and locker and nothing
	// else changes.
	sched := scheduler.New(lock.NewMemory())
	di.MustSupply(app.Container, events.New())
	di.MustSupply(app.Container, sched)
	di.MustSupply[cache.Cache](app.Container, cache.NewMemory(time.Minute))

	// The scheduler starts after the listener is up and drains on the way
	// down, because it is registered as a lifecycle participant.
	app.Lifecycle.Observe("scheduler", sched)

	if err := app.Mount(todo.Descriptor()); err != nil {
		return err
	}

	logger.Info("route table\n" + app.RouteTable())
	return app.Run(context.Background())
}

// headerIdentity reads the caller from request headers.
//
// A REAL service verifies a signed token here and never trusts a header for
// identity: an X-Account-Id a client can set is an open door to every other
// account's data. This example does it so that it runs with nothing else
// installed. Do not copy the mechanism — copy the shape, which is that
// Identity establishes who the caller is and a guard decides what they may
// do.
func headerIdentity(c fiber.Ctx) (ctxkit.Tenant, ctxkit.Actor, error) {
	var tenant ctxkit.Tenant
	var actor ctxkit.Actor

	if raw := strings.TrimSpace(c.Get("X-Account-Id")); raw != "" {
		if id, err := uuid.Parse(raw); err == nil {
			tenant.AccountID = id
		}
	}
	if raw := strings.TrimSpace(c.Get("X-User-Id")); raw != "" {
		if id, err := uuid.Parse(raw); err == nil {
			actor.UserID = id
		}
	}
	return tenant, actor, nil
}
