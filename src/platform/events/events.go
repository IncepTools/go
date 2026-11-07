// Package events is a typed, in-process event bus: no `any` at the call
// site, no type assertion in the handler, and the failure modes handled.
//
// Use it to decouple a side effect from the thing that caused it: checkout
// publishes OrderPlaced, and the audit log, the email sender and the usage
// meter each subscribe without the checkout code importing any of them.
//
// Three things it does that a channel and a goroutine do not:
//
//   - A handler panic is contained. One bad subscriber cannot take down the
//     publisher, which in an HTTP service is the request.
//   - Async handlers get a DETACHED context. Handing a handler the request
//     context means the side effect is cancelled when the client disconnects
//     — the notification silently never sends.
//   - Publish waits for the synchronous handlers and returns their errors, so
//     an effect that MUST happen can be made to block the write that caused it.
//
// What it is not: durable. The process dying loses every unhandled event. When
// the effect must survive a crash, write it to a table in the same transaction
// as the state change and publish from there.
package events

import (
	"context"
	"fmt"
	"reflect"
	"runtime/debug"
	"sync"

	"go.opentelemetry.io/otel/attribute"

	"github.com/IncepTools/go/src/core/ctxkit"
	"github.com/IncepTools/go/src/core/logx"
	"github.com/IncepTools/go/src/core/telemetry"
)

// Handler reacts to one event.
type Handler[T any] func(ctx context.Context, event T) error

// Mode selects how a subscriber runs.
type Mode int

const (
	// Async runs the handler in its own goroutine with a detached context.
	// Publish does not wait for it and never sees its error.
	//
	// This is the right default for a side effect: an audit write must not
	// slow down, or fail, the request that produced it.
	Async Mode = iota
	// Sync runs the handler inline. Publish waits and collects the error.
	//
	// Use it only when the publisher genuinely must not proceed if the
	// handler fails — and prefer a direct call in that case, because a
	// required dependency hidden behind an event bus is harder to follow than
	// a function call.
	Sync
)

type subscription struct {
	name string
	mode Mode
	call func(ctx context.Context, event any) error
}

// Bus dispatches events to subscribers. The zero value is usable.
type Bus struct {
	mu   sync.RWMutex
	subs map[reflect.Type][]subscription
	wg   sync.WaitGroup
}

// New returns an empty bus.
func New() *Bus { return &Bus{subs: map[reflect.Type][]subscription{}} }

func (b *Bus) ensure() {
	if b.subs == nil {
		b.subs = map[reflect.Type][]subscription{}
	}
}

// On subscribes to events of type T. name identifies the subscriber in logs,
// which is the difference between "an event handler failed" and a line you
// can act on.
func On[T any](b *Bus, name string, mode Mode, h Handler[T]) {
	t := reflect.TypeOf((*T)(nil)).Elem()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.ensure()
	b.subs[t] = append(b.subs[t], subscription{
		name: name,
		mode: mode,
		call: func(ctx context.Context, ev any) error {
			typed, ok := ev.(T)
			if !ok {
				return fmt.Errorf("events: %s received %T, expected %s", name, ev, t)
			}
			return h(ctx, typed)
		},
	})
}

// Publish dispatches an event.
//
// It returns the joined errors of the SYNCHRONOUS subscribers only. Async
// errors are logged by the bus, because by definition nobody is waiting for
// them.
func Publish[T any](ctx context.Context, b *Bus, event T) error {
	if b == nil {
		return nil
	}
	t := reflect.TypeOf((*T)(nil)).Elem()
	b.mu.RLock()
	subs := append([]subscription(nil), b.subs[t]...)
	b.mu.RUnlock()

	if len(subs) == 0 {
		return nil
	}

	var errs []error
	for _, s := range subs {
		if s.mode == Sync {
			if err := b.invoke(ctx, s, event); err != nil {
				errs = append(errs, err)
			}
			continue
		}
		// Detached: keeps the trace id and the tenant, drops the deadline and
		// the cancellation. A side effect must not die because the client
		// hung up.
		actx := ctxkit.Detach(ctx)
		b.wg.Add(1)
		go func(s subscription) {
			defer b.wg.Done()
			if err := b.invoke(actx, s, event); err != nil {
				logx.C(actx).Error("event handler failed",
					"handler", s.name, "event", t.String(), "error", err.Error())
			}
		}(s)
	}
	return join(errs)
}

func (b *Bus) invoke(ctx context.Context, s subscription, event any) (err error) {
	ctx, span := telemetry.Start(ctx, "event.handler",
		attribute.String("event.type", reflect.TypeOf(event).String()),
		attribute.String("event.handler", s.name),
		attribute.String("event.mode", modeName(s.mode)),
	)
	defer span.End()
	defer func() {
		if r := recover(); r != nil {
			logx.C(ctx).Error("event handler panicked",
				"handler", s.name, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			err = fmt.Errorf("events: handler %s panicked", s.name)
		}
		telemetry.RecordError(span, err)
	}()
	err = s.call(ctx, event)
	return err
}

func modeName(mode Mode) string {
	if mode == Sync {
		return "sync"
	}
	return "async"
}

// Wait blocks until every async handler started so far has finished.
//
// Call it from a shutdown hook. Without it, a SIGTERM during a burst of
// publishes drops the side effects that were still in flight.
func (b *Bus) Wait() {
	if b == nil {
		return
	}
	b.wg.Wait()
}

// OnShutdown implements lifecycle.Shutdowner, so a bus registered as a hook
// drains itself on the way down.
func (b *Bus) OnShutdown(ctx context.Context) error {
	done := make(chan struct{})
	go func() { b.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("events: handlers still running at the drain deadline")
	}
}

// Subscribers lists the handler names registered for each event type, for
// diagnostics.
func (b *Bus) Subscribers() map[string][]string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := map[string][]string{}
	for t, subs := range b.subs {
		for _, s := range subs {
			out[t.String()] = append(out[t.String()], s.name)
		}
	}
	return out
}

func join(errs []error) error {
	switch len(errs) {
	case 0:
		return nil
	case 1:
		return errs[0]
	default:
		msg := "events: "
		for i, e := range errs {
			if i > 0 {
				msg += "; "
			}
			msg += e.Error()
		}
		return fmt.Errorf("%s", msg)
	}
}
