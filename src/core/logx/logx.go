// Package logx is the application logger: log/slog, with the request-scoped
// identity from ctxkit attached automatically.
//
// There is one rule worth stating. Log the CAUSE, never the response. An
// *errs.Error carries a caller-safe Message and an internal Cause; the client
// gets the first, this package gets both. Logging only what you returned is how
// a production incident becomes a trace id with no explanation behind it.
package logx

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/IncepTools/go/src/core/ctxkit"
	"github.com/IncepTools/go/src/core/errs"
)

// Format selects the encoder.
type Format string

const (
	// FormatJSON is the production encoder: one JSON object per line.
	FormatJSON Format = "json"
	// FormatText is the development encoder: human-readable key=value.
	FormatText Format = "text"
)

// Options configures a logger.
type Options struct {
	Level  slog.Level
	Format Format
	// Writer defaults to os.Stdout. Logs go to stdout, not stderr: stderr is
	// for the process dying, and mixing the two makes a crash hard to find.
	Writer io.Writer
	// Service is attached to every record as "service".
	Service string
	// AddSource includes file:line. Costs a little; worth it outside hot loops.
	AddSource bool
}

// ParseLevel maps a config string ("debug", "info", "warn", "error") to a
// slog.Level, defaulting to Info for anything unrecognised.
func ParseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// New builds a *slog.Logger from opts.
func New(opts Options) *slog.Logger {
	w := opts.Writer
	if w == nil {
		w = os.Stdout
	}
	ho := &slog.HandlerOptions{Level: opts.Level, AddSource: opts.AddSource}
	var h slog.Handler
	if opts.Format == FormatText {
		h = slog.NewTextHandler(w, ho)
	} else {
		h = slog.NewJSONHandler(w, ho)
	}
	l := slog.New(h)
	if opts.Service != "" {
		l = l.With("service", opts.Service)
	}
	return l
}

type loggerKey struct{}

// Into returns a context carrying l, so a deep call can log with the request's
// own fields without threading a logger through every signature.
func Into(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, l)
}

// From returns the logger on ctx, falling back to slog.Default. It never
// returns nil.
func From(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.Default()
}

// C returns the context logger with the request-scoped identity attached:
// trace id, request id, account and user when present.
//
// Use it at the point of logging — `logx.C(ctx).Warn(...)` — rather than
// building a decorated logger and passing it around.
func C(ctx context.Context) *slog.Logger {
	l := From(ctx)
	if t := ctxkit.Trace(ctx); t != "" {
		l = l.With("traceId", t)
	}
	if r := ctxkit.RequestID(ctx); r != "" {
		l = l.With("requestId", r)
	}
	if tn := ctxkit.TenantOf(ctx); !tn.Zero() {
		l = l.With("accountId", tn.AccountID.String())
	}
	if a := ctxkit.ActorOf(ctx); a.UserID.String() != "00000000-0000-0000-0000-000000000000" {
		l = l.With("userId", a.UserID.String())
	}
	return l
}

// Err logs an error with everything the client did not get: the kind, the
// code, the internal cause and the structured meta.
//
// It is the counterpart of the web layer's sanitised response — between the
// two, every failure is both safe on the wire and fully explained in the log.
func Err(ctx context.Context, msg string, err error, args ...any) {
	if err == nil {
		return
	}
	e := errs.From(err)
	attrs := append([]any{}, args...)
	attrs = append(attrs, "kind", string(errs.KindOf(e)))
	if e.Code != "" {
		attrs = append(attrs, "code", e.Code)
	}
	if e.Cause != nil {
		attrs = append(attrs, "cause", e.Cause.Error())
	}
	for k, v := range e.Meta {
		attrs = append(attrs, k, v)
	}
	level := slog.LevelError
	switch errs.KindOf(e) {
	case errs.KindInvalid, errs.KindNotFound, errs.KindUnauthorized, errs.KindForbidden,
		errs.KindConflict, errs.KindExhausted, errs.KindPayment:
		// Caller-caused. Real, but not a defect — do not page on these.
		level = slog.LevelWarn
	}
	C(ctx).Log(ctx, level, msg, append(attrs, "error", e.Message)...)
}
