// Command blog is a database-backed inceptools service.
//
// Where the todo example keeps everything in memory, this one shows the
// data layer: versioned SQL, a scoped repository, a transaction that spans
// a state change and an outbox message, and a relay that delivers it.
//
// It uses SQLite, so it still needs nothing installed:
//
//	go run ./blog
//
//	ACC=11111111-1111-1111-1111-111111111111
//	USR=22222222-2222-2222-2222-222222222222
//	H=(-H "Content-Type: application/json" -H "X-Account-Id: $ACC" -H "X-User-Id: $USR")
//
//	curl -X POST localhost:8081/api/v1/blog/posts "${H[@]}" \
//	     -d '{"title":"Hello, world","body":"First post."}'
//
//	curl -X POST localhost:8081/api/v1/blog/posts/<id>/publish "${H[@]}"
//	# the relay logs the delivery a moment later
//
//	curl "localhost:8081/api/v1/blog/posts?published=true" "${H[@]}"
package main

import (
	"context"
	"database/sql"
	"embed"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	inceptools "github.com/IncepTools/go"
	"github.com/IncepTools/go/examples/blog/apps/blog"
	"github.com/IncepTools/go/examples/blog/apps/blog/posts/dto"
	"github.com/IncepTools/go/src/core/ctxkit"
	"github.com/IncepTools/go/src/core/di"
	"github.com/IncepTools/go/src/core/logx"
	"github.com/IncepTools/go/src/reliability/outbox"
	"github.com/IncepTools/go/src/web/binding"
	"github.com/IncepTools/go/src/web/health"
	"github.com/IncepTools/go/src/web/middleware"
)

// migrationFS carries the SQL into the binary, so `go run` works from any
// directory. A real service applies these with `icpt migrate` instead.
//
//go:embed migrations/*.sql
var migrationFS embed.FS

func main() {
	if err := run(); err != nil {
		log.Printf("fatal: %v", err)
		os.Exit(1)
	}
}

func run() error {
	logger := logx.New(logx.Options{
		Level: slog.LevelDebug, Format: logx.FormatText, Service: "blog",
	})
	slog.SetDefault(logger)

	// A custom validation rule, registered once at boot. Always register a
	// message with it: without one the rule renders as "slug is invalid",
	// which tells a user nothing about how to fix it.
	if err := binding.RegisterRule("slug", func(fl validator.FieldLevel) bool {
		return dto.Slugify(fl.Field().String()) == fl.Field().String()
	}); err != nil {
		return err
	}
	binding.RegisterMessage("slug", func(validator.FieldError) string {
		return "slug may contain only lowercase letters, digits and single hyphens"
	})

	dsn := os.Getenv("BLOG_DATABASE_URL")
	if dsn == "" {
		dsn = filepath.Join(os.TempDir(), "inceptools-blog.db")
	}
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Warn),
	})
	if err != nil {
		return err
	}
	if err := applyMigrations(db); err != nil {
		return err
	}
	logger.Info("database ready", "dsn", dsn)

	app := inceptools.New(inceptools.Options{
		Service: "blog",
		Version: "1.0.0",
		Addr:    ":8081",
		Prefix:  "/api/v1",
		Logger:  logger,
		Middleware: middleware.ChainOptions{
			Timeout:  15 * time.Second,
			Identity: headerIdentity,
			AccessLog: middleware.AccessLogOptions{
				LogSuccess: true,
				SkipPaths:  []string{"/healthz", "/readyz"},
			},
		},
	})

	di.MustSupply(app.Container, db)

	// The relay delivers what the publish transaction recorded. It is safe
	// on every replica: each pass claims rows with FOR UPDATE SKIP LOCKED,
	// so two relays take disjoint batches.
	relay := outbox.NewRelay(outbox.RelayOptions{
		DB:        db,
		Interval:  2 * time.Second,
		Publisher: outbox.PublisherFunc(deliver),
	})
	app.Lifecycle.Observe("outbox", relay)

	app.Health.Register(health.Of("database", health.Critical, func(ctx context.Context) error {
		sqlDB, err := db.DB()
		if err != nil {
			return err
		}
		return sqlDB.PingContext(ctx)
	}))

	if err := app.Mount(blog.Descriptor()); err != nil {
		return err
	}

	logger.Info("route table\n" + app.RouteTable())
	return app.Run(context.Background())
}

// deliver stands in for a broker publish, a webhook or an email.
//
// An error means "try again later" and the relay reschedules with backoff.
// Returning outbox.ErrDrop would stop it retrying a message that can never
// succeed.
func deliver(ctx context.Context, m outbox.Message) error {
	var ev dto.PostPublished
	if err := json.Unmarshal(m.Payload, &ev); err != nil {
		// A payload from an older deployment that this code cannot read
		// will never become readable. Drop it rather than retrying forever.
		return fmt.Errorf("%w: %v", outbox.ErrDrop, err)
	}
	logx.C(ctx).Info("delivered outbox message",
		"topic", m.Topic, "postId", ev.PostID, "slug", ev.Slug)
	return nil
}

// applyMigrations runs the SQL in migrations/.
//
// A real service runs `icpt migrate` as a deploy step, against a ledger
// that records what was applied. This example applies the same files itself
// so that `go run ./blog` is the only command you need — it is a shortcut
// for the example, not a pattern to copy.
func applyMigrations(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	if applied, err := alreadyApplied(sqlDB); err != nil {
		return err
	} else if applied {
		return nil
	}

	raw, err := migrationFS.ReadFile("migrations/000001_init.up.sql")
	if err != nil {
		return err
	}
	for _, stmt := range strings.Split(string(raw), ";") {
		if strings.TrimSpace(stripComments(stmt)) == "" {
			continue
		}
		if _, err := sqlDB.Exec(stmt); err != nil {
			return fmt.Errorf("applying the migration: %w", err)
		}
	}
	return nil
}

func alreadyApplied(db *sql.DB) (bool, error) {
	var name string
	err := db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'posts'`).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func stripComments(s string) string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// headerIdentity reads the caller from request headers.
//
// A REAL service verifies a signed token here and never trusts a header for
// identity. This example does it so that it runs with nothing installed;
// copy the shape, not the mechanism.
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
