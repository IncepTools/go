// Package breaker stops calling a dependency that is clearly down.
//
// The problem it solves is not the failing dependency — it is what your
// service does about it. Every request waits for a three-second timeout, so
// every worker is blocked waiting, so the service stops answering the
// endpoints that do not need that dependency at all. One failing downstream
// has taken out everything.
//
// An open breaker fails those calls immediately, with KindUnavailable, which
// frees the workers and gives the dependency room to recover.
//
//	closed    calls pass. Consecutive failures count toward the threshold.
//	open      calls fail at once. After Cooldown, one probe is allowed.
//	half-open one call probes. Success closes; failure re-opens.
package breaker

import (
	"context"
	"sync"
	"time"

	"github.com/IncepTools/go/src/core/errs"
	"github.com/IncepTools/go/src/core/logx"
)

// State is the breaker's position.
type State string

const (
	// Closed is normal operation: calls pass, and consecutive failures count
	// toward the threshold.
	Closed State = "closed"
	// Open is failing fast: calls are refused without touching the
	// dependency, which is what frees the workers it was blocking.
	Open State = "open"
	// HalfOpen is one probe in flight. Success closes the breaker; failure
	// re-opens it.
	HalfOpen State = "half-open"
)

// Options configures a breaker.
type Options struct {
	// Name identifies the dependency in logs and in the error.
	Name string
	// Threshold is the consecutive failures that open the breaker. Default 5.
	Threshold int
	// Cooldown is how long it stays open before probing. Default 30s.
	Cooldown time.Duration
	// Counts decides whether an error counts toward the threshold.
	//
	// The default counts only unavailability and timeouts. A 404 from a
	// dependency is a perfectly healthy answer and must not open a breaker —
	// counting it means a client asking for missing things repeatedly takes
	// the dependency out for everyone.
	Counts func(error) bool
	// OnStateChange is called on every transition.
	OnStateChange func(from, to State)
}

// Breaker guards one dependency. Create one per dependency, not one per
// service: a shared breaker lets an unrelated failure block a healthy call.
type Breaker struct {
	opt Options

	mu       sync.Mutex
	state    State
	failures int
	openedAt time.Time
	probing  bool
}

// New builds a breaker.
func New(opt Options) *Breaker {
	if opt.Threshold <= 0 {
		opt.Threshold = 5
	}
	if opt.Cooldown <= 0 {
		opt.Cooldown = 30 * time.Second
	}
	if opt.Counts == nil {
		opt.Counts = defaultCounts
	}
	if opt.Name == "" {
		opt.Name = "dependency"
	}
	return &Breaker{opt: opt, state: Closed}
}

func defaultCounts(err error) bool {
	switch errs.KindOf(err) {
	case errs.KindUnavailable, errs.KindTimeout, errs.KindInternal:
		return true
	default:
		return false
	}
}

// State reports the current state.
func (b *Breaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

// Do runs fn unless the breaker is open.
func (b *Breaker) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	allowed, probe := b.allow()
	if !allowed {
		return errs.Unavailable("%s is unavailable", b.opt.Name).
			WithCode("CIRCUIT_OPEN").
			WithMeta("breaker", b.opt.Name)
	}
	err := fn(ctx)
	b.record(ctx, err, probe)
	return err
}

// Value is Do for a function that returns a result.
func Value[T any](ctx context.Context, b *Breaker, fn func(ctx context.Context) (T, error)) (T, error) {
	var out T
	err := b.Do(ctx, func(ctx context.Context) error {
		v, err := fn(ctx)
		if err != nil {
			return err
		}
		out = v
		return nil
	})
	return out, err
}

func (b *Breaker) allow() (allowed, probe bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case Closed:
		return true, false
	case Open:
		if time.Since(b.openedAt) < b.opt.Cooldown {
			return false, false
		}
		b.transition(HalfOpen)
		b.probing = true
		return true, true
	default: // HalfOpen
		// Exactly ONE probe at a time. Letting every waiting request through
		// on the cooldown boundary is a stampede onto a dependency that has
		// not recovered yet.
		if b.probing {
			return false, false
		}
		b.probing = true
		return true, true
	}
}

func (b *Breaker) record(ctx context.Context, err error, probe bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if probe {
		b.probing = false
	}

	if err == nil || !b.opt.Counts(err) {
		if b.state != Closed {
			b.transition(Closed)
		}
		b.failures = 0
		return
	}

	b.failures++
	if b.state == HalfOpen {
		b.openedAt = time.Now()
		b.transition(Open)
		logx.C(ctx).Warn("circuit re-opened: the probe failed", "breaker", b.opt.Name, "error", err.Error())
		return
	}
	if b.state == Closed && b.failures >= b.opt.Threshold {
		b.openedAt = time.Now()
		b.transition(Open)
		logx.C(ctx).Error("circuit opened",
			"breaker", b.opt.Name, "failures", b.failures, "cooldown", b.opt.Cooldown.String(), "error", err.Error())
	}
}

// transition must be called with the mutex held.
func (b *Breaker) transition(to State) {
	from := b.state
	if from == to {
		return
	}
	b.state = to
	if to == Closed {
		b.failures = 0
		b.probing = false
	}
	if b.opt.OnStateChange != nil {
		// Called under the lock, so the callback must be cheap and must not
		// call back into the breaker. Documented rather than solved: a
		// goroutine per transition would reorder the notifications.
		b.opt.OnStateChange(from, to)
	}
}
