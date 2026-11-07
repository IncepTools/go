// Package lifecycle gives a module three moments — init, ready, shutdown —
// and the signal handling that makes shutdown actually happen.
//
// The ordering rule is the whole point. Hooks run in registration order going
// up and in REVERSE registration order coming down, so a module can safely
// assume that anything it was wired after is still alive while it drains.
package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// Initializer runs after the container is wired and before the server accepts
// traffic. Open pools, warm caches, verify a dependency answers.
//
// An error here fails the boot. That is intended: a service that cannot reach
// its database should not report healthy and serve 500s.
type Initializer interface {
	OnInit(ctx context.Context) error
}

// Ready runs after the listener is up. Start background loops here, not in
// OnInit — a reconciler that begins before the process is serving will race
// the boot it is part of.
type Ready interface {
	OnReady(ctx context.Context) error
}

// Shutdowner runs on the way down, in reverse registration order, under the
// drain deadline. Stop loops, flush buffers, close clients.
type Shutdowner interface {
	OnShutdown(ctx context.Context) error
}

// Hook is one named participant in the lifecycle. A hook need only implement
// the phases it cares about.
type Hook struct {
	Name     string
	Init     func(ctx context.Context) error
	Ready    func(ctx context.Context) error
	Shutdown func(ctx context.Context) error
}

// Registry collects hooks and runs each phase in the right order.
//
// The zero value is ready to use.
type Registry struct {
	mu    sync.Mutex
	hooks []Hook
	done  []int // indices that completed Init, for symmetric teardown
}

// Add registers a hook. Order matters: later hooks initialise after earlier
// ones and shut down before them.
func (r *Registry) Add(h Hook) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if h.Name == "" {
		h.Name = fmt.Sprintf("hook-%d", len(r.hooks))
	}
	r.hooks = append(r.hooks, h)
}

// Observe registers whichever lifecycle interfaces v implements. Pass a module
// or a provider and the registry works out what it participates in; passing
// something that implements none is a no-op, not an error.
func (r *Registry) Observe(name string, v any) {
	h := Hook{Name: name}
	if i, ok := v.(Initializer); ok {
		h.Init = i.OnInit
	}
	if i, ok := v.(Ready); ok {
		h.Ready = i.OnReady
	}
	if i, ok := v.(Shutdowner); ok {
		h.Shutdown = i.OnShutdown
	}
	if h.Init == nil && h.Ready == nil && h.Shutdown == nil {
		return
	}
	r.Add(h)
}

// Names lists registered hooks in order.
func (r *Registry) Names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.hooks))
	for i, h := range r.hooks {
		out[i] = h.Name
	}
	return out
}

// Init runs every Init hook in registration order and STOPS at the first
// failure, returning it.
//
// It stops rather than continuing because the hooks after a failed one were
// written assuming it succeeded. Whatever did initialise is recorded, so a
// following Shutdown unwinds exactly that much and no more.
func (r *Registry) Init(ctx context.Context) error {
	r.mu.Lock()
	hooks := append([]Hook(nil), r.hooks...)
	r.mu.Unlock()

	for i, h := range hooks {
		if h.Init == nil {
			r.markDone(i)
			continue
		}
		if err := h.Init(ctx); err != nil {
			return fmt.Errorf("lifecycle: %s failed to initialise: %w", h.Name, err)
		}
		r.markDone(i)
	}
	return nil
}

func (r *Registry) markDone(i int) {
	r.mu.Lock()
	r.done = append(r.done, i)
	r.mu.Unlock()
}

// Ready runs every Ready hook in registration order. Unlike Init it runs them
// ALL and joins the errors: the process is already serving, and refusing to
// start a second background loop because the first one stuttered helps nobody.
func (r *Registry) Ready(ctx context.Context) error {
	r.mu.Lock()
	hooks := append([]Hook(nil), r.hooks...)
	r.mu.Unlock()

	var errs []error
	for _, h := range hooks {
		if h.Ready == nil {
			continue
		}
		if err := h.Ready(ctx); err != nil {
			errs = append(errs, fmt.Errorf("lifecycle: %s failed on ready: %w", h.Name, err))
		}
	}
	return errors.Join(errs...)
}

// Shutdown runs the Shutdown hook of everything that initialised, in reverse
// order, and joins the errors — one slow client must not keep the rest from
// draining.
func (r *Registry) Shutdown(ctx context.Context) error {
	r.mu.Lock()
	hooks := append([]Hook(nil), r.hooks...)
	done := append([]int(nil), r.done...)
	r.mu.Unlock()

	var errs []error
	for i := len(done) - 1; i >= 0; i-- {
		h := hooks[done[i]]
		if h.Shutdown == nil {
			continue
		}
		if err := h.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("lifecycle: %s failed to shut down: %w", h.Name, err))
		}
	}
	return errors.Join(errs...)
}

// DefaultDrainTimeout is how long Run waits for in-flight work after a signal.
const DefaultDrainTimeout = 25 * time.Second

// Signals are the signals Run treats as "shut down cleanly".
var Signals = []os.Signal{syscall.SIGINT, syscall.SIGTERM}

// Run is the standard main loop: initialise, serve, wait for a signal, drain.
//
//   - serve blocks until the listener stops. A nil return means an intentional
//     stop; an error means the listener died and the process should exit
//     non-zero.
//   - stop is called once a signal arrives, and must make serve return.
//
// The drain context is a FRESH one with its own deadline, not a child of the
// cancelled parent. Deriving it from the cancelled context is the classic bug:
// every shutdown hook gets a context that is already done and skips its work.
func Run(ctx context.Context, r *Registry, serve func() error, stop func(context.Context) error, drain time.Duration) error {
	if drain <= 0 {
		drain = DefaultDrainTimeout
	}
	if err := r.Init(ctx); err != nil {
		// Unwind whatever came up before the failure.
		dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), drain)
		defer cancel()
		return errors.Join(err, r.Shutdown(dctx))
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- serve() }()

	if err := r.Ready(ctx); err != nil {
		return err
	}

	sigCtx, stopSignals := signal.NotifyContext(ctx, Signals...)
	defer stopSignals()

	var runErr error
	select {
	case err := <-serveErr:
		runErr = err
	case <-sigCtx.Done():
		stopSignals()
	}

	dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), drain)
	defer cancel()

	var errs []error
	if runErr != nil {
		errs = append(errs, runErr)
	}
	if stop != nil {
		if err := stop(dctx); err != nil {
			errs = append(errs, fmt.Errorf("lifecycle: stopping the listener: %w", err))
		}
	}
	if err := r.Shutdown(dctx); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}
