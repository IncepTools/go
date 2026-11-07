package health_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/IncepTools/go/web/health"
)

func TestOptionalFailureDegradesRatherThanDowns(t *testing.T) {
	r := health.New("svc", "1.0")
	r.Register(
		health.Of("db", health.Critical, func(context.Context) error { return nil }),
		health.Of("analytics", health.Optional, func(context.Context) error { return errors.New("timeout") }),
	)

	rep := r.Run(context.Background())
	if rep.Status != health.StatusDegraded {
		t.Fatalf("status = %s, want DEGRADED — an optional dependency must not take the service out of rotation", rep.Status)
	}
	if rep.Checks["analytics"].Status != health.StatusDegraded {
		t.Fatalf("analytics = %+v", rep.Checks["analytics"])
	}
}

func TestCriticalFailureDowns(t *testing.T) {
	r := health.New("svc", "1.0")
	r.Register(health.Of("db", health.Critical, func(context.Context) error { return errors.New("refused") }))

	if got := r.Run(context.Background()).Status; got != health.StatusDown {
		t.Fatalf("status = %s, want DOWN", got)
	}
}

func TestChecksRunConcurrently(t *testing.T) {
	r := health.New("svc", "1.0")
	r.Timeout = time.Second
	for i := range 6 {
		_ = i
		r.Register(health.Of("slow", health.Critical, func(ctx context.Context) error {
			select {
			case <-time.After(80 * time.Millisecond):
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}))
	}

	start := time.Now()
	r.Run(context.Background())
	elapsed := time.Since(start)

	// Sequentially this is ~480ms. Concurrently it is ~80ms.
	if elapsed > 300*time.Millisecond {
		t.Fatalf("took %v; six 80ms checks must not run one after another", elapsed)
	}
}

func TestSlowCheckIsBoundedByTheTimeout(t *testing.T) {
	r := health.New("svc", "1.0")
	r.Timeout = 20 * time.Millisecond
	r.Register(health.Of("hang", health.Critical, func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}))

	start := time.Now()
	rep := r.Run(context.Background())
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("the registry waited past its own per-check timeout")
	}
	if rep.Status != health.StatusDown {
		t.Fatalf("status = %s", rep.Status)
	}
}

func TestPanickingCheckDoesNotCrashTheReport(t *testing.T) {
	r := health.New("svc", "1.0")
	r.Register(health.Of("boom", health.Critical, func(context.Context) error { panic("nope") }))

	rep := r.Run(context.Background())
	if rep.Status != health.StatusDown {
		t.Fatalf("status = %s, want DOWN", rep.Status)
	}
}

func TestEmptyRegistryIsUp(t *testing.T) {
	if got := health.New("svc", "1.0").Run(context.Background()).Status; got != health.StatusUp {
		t.Fatalf("status = %s, want UP", got)
	}
}
