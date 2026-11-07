// Package retry re-attempts work that failed for a reason that might not
// recur.
//
// The whole subject is one judgement call: retrying the wrong thing is worse
// than not retrying at all. A retried POST that actually succeeded creates a
// second resource. A retry loop with no jitter turns a brief dependency blip
// into a synchronised stampede that keeps it down. A retry of a 400 is three
// requests that were always going to fail.
//
// So this package defaults to NOT retrying, and asks you to say what is
// retryable. Retryable() covers the usual answer: timeouts, unavailability,
// and serialization conflicts — never a client error, never an unclassified
// one.
package retry

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/IncepTools/go/src/core/errs"
	"github.com/IncepTools/go/src/core/logx"
)

// Policy describes how to retry.
type Policy struct {
	// MaxAttempts counts the FIRST try. 3 means one try and two retries.
	// Zero or one disables retrying.
	MaxAttempts int
	// BaseDelay is the first wait. Each subsequent wait doubles it.
	BaseDelay time.Duration
	// MaxDelay caps the exponential growth.
	MaxDelay time.Duration
	// Jitter randomises each wait by up to ±50%.
	//
	// Leave it ON. Without jitter, every client that failed at the same
	// moment retries at the same moment, and the dependency that was briefly
	// slow is now reliably overloaded.
	NoJitter bool
	// Retryable decides whether an error is worth another attempt. Defaults
	// to Retryable.
	Retryable func(error) bool
	// OnRetry is called before each wait, for logging or a metric.
	OnRetry func(attempt int, err error, wait time.Duration)
}

// Default is a sensible policy for a network call: three attempts, 100ms
// growing to 2s, jittered.
func Default() Policy {
	return Policy{MaxAttempts: 3, BaseDelay: 100 * time.Millisecond, MaxDelay: 2 * time.Second}
}

// Retryable reports whether an error is worth retrying: a timeout, an
// unavailable dependency, or a serialization conflict.
//
// Everything else is not. In particular an unclassified error is NOT
// retryable — if nobody has decided what it means, it must not be assumed
// safe to repeat.
func Retryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		// The caller's deadline, not the dependency's. Retrying cannot help:
		// there is no time left.
		return false
	}
	switch errs.KindOf(err) {
	case errs.KindUnavailable, errs.KindTimeout:
		return true
	case errs.KindConflict:
		// Only the specific conflict that means "two transactions raced";
		// a uniqueness conflict will fail identically forever.
		return errs.CodeOf(err) == "CONCURRENT_MODIFICATION"
	default:
		return false
	}
}

// Do runs fn until it succeeds, the policy is exhausted, or the error is not
// retryable. It returns the LAST error.
//
// The context is honoured between attempts: a cancelled caller stops waiting
// immediately rather than sleeping out the backoff.
func Do(ctx context.Context, p Policy, fn func(ctx context.Context) error) error {
	attempts := max(p.MaxAttempts, 1)
	retryable := p.Retryable
	if retryable == nil {
		retryable = Retryable
	}

	var err error
	for attempt := 1; attempt <= attempts; attempt++ {
		if ctxErr := ctx.Err(); ctxErr != nil {
			if err == nil {
				return ctxErr
			}
			return err
		}
		err = fn(ctx)
		if err == nil {
			return nil
		}
		if attempt == attempts || !retryable(err) {
			return err
		}

		wait := p.wait(attempt)
		if p.OnRetry != nil {
			p.OnRetry(attempt, err, wait)
		} else {
			logx.C(ctx).Debug("retrying", "attempt", attempt, "waitMs", wait.Milliseconds(), "error", err.Error())
		}

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return err
		case <-timer.C:
		}
	}
	return err
}

// Value is Do for a function that returns a result.
func Value[T any](ctx context.Context, p Policy, fn func(ctx context.Context) (T, error)) (T, error) {
	var out T
	err := Do(ctx, p, func(ctx context.Context) error {
		v, err := fn(ctx)
		if err != nil {
			return err
		}
		out = v
		return nil
	})
	return out, err
}

func (p Policy) wait(attempt int) time.Duration {
	base := p.BaseDelay
	if base <= 0 {
		base = 100 * time.Millisecond
	}
	d := base << (attempt - 1)
	if d <= 0 { // overflow on a very large attempt count
		d = p.MaxDelay
	}
	if p.MaxDelay > 0 && d > p.MaxDelay {
		d = p.MaxDelay
	}
	if p.NoJitter {
		return d
	}
	// ±50%, so two clients that failed together do not retry together.
	half := int64(d / 2)
	if half <= 0 {
		return d
	}
	// math/rand is correct here: the jitter decorrelates retries, it is not
	// a secret.
	return time.Duration(half + rand.Int64N(half*2+1)) //nolint:gosec
}
