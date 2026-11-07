package breaker_test

import (
	"context"
	"testing"
	"time"

	"github.com/IncepTools/go/core/errs"
	"github.com/IncepTools/go/reliability/breaker"
)

func fail(context.Context) error { return errs.Unavailable("down") }
func ok(context.Context) error   { return nil }

func TestOpensAfterTheThreshold(t *testing.T) {
	b := breaker.New(breaker.Options{Name: "dep", Threshold: 3, Cooldown: time.Minute})
	ctx := context.Background()

	for range 3 {
		_ = b.Do(ctx, fail)
	}
	if b.State() != breaker.Open {
		t.Fatalf("state = %s, want open", b.State())
	}

	calls := 0
	err := b.Do(ctx, func(context.Context) error { calls++; return nil })
	if calls != 0 {
		t.Fatal("an open breaker called the dependency")
	}
	if errs.CodeOf(err) != "CIRCUIT_OPEN" {
		t.Fatalf("code = %q, want CIRCUIT_OPEN", errs.CodeOf(err))
	}
	if !errs.Is(err, errs.KindUnavailable) {
		t.Fatalf("kind = %q, want unavailable", errs.KindOf(err))
	}
}

func TestClientErrorsDoNotOpenTheBreaker(t *testing.T) {
	b := breaker.New(breaker.Options{Threshold: 2, Cooldown: time.Minute})
	ctx := context.Background()

	for range 10 {
		_ = b.Do(ctx, func(context.Context) error { return errs.NotFound("no such thing") })
	}
	if b.State() != breaker.Closed {
		t.Fatal("a stream of 404s opened the breaker; a dependency answering correctly is healthy")
	}
}

func TestHalfOpenProbeClosesOnSuccess(t *testing.T) {
	b := breaker.New(breaker.Options{Threshold: 1, Cooldown: 10 * time.Millisecond})
	ctx := context.Background()

	_ = b.Do(ctx, fail)
	if b.State() != breaker.Open {
		t.Fatal("did not open")
	}
	time.Sleep(20 * time.Millisecond)

	if err := b.Do(ctx, ok); err != nil {
		t.Fatalf("the probe was refused: %v", err)
	}
	if b.State() != breaker.Closed {
		t.Fatalf("state = %s, want closed after a successful probe", b.State())
	}
}

func TestHalfOpenProbeReopensOnFailure(t *testing.T) {
	b := breaker.New(breaker.Options{Threshold: 1, Cooldown: 10 * time.Millisecond})
	ctx := context.Background()

	_ = b.Do(ctx, fail)
	time.Sleep(20 * time.Millisecond)
	_ = b.Do(ctx, fail)

	if b.State() != breaker.Open {
		t.Fatalf("state = %s, want open after a failed probe", b.State())
	}
}

func TestOnlyOneProbeIsAllowedAtATime(t *testing.T) {
	b := breaker.New(breaker.Options{Threshold: 1, Cooldown: 10 * time.Millisecond})
	ctx := context.Background()

	_ = b.Do(ctx, fail)
	time.Sleep(20 * time.Millisecond)

	// Start a probe and hold it open.
	started := make(chan struct{})
	finish := make(chan struct{})
	go func() {
		_ = b.Do(ctx, func(context.Context) error {
			close(started)
			<-finish
			return nil
		})
	}()
	<-started

	calls := 0
	_ = b.Do(ctx, func(context.Context) error { calls++; return nil })
	if calls != 0 {
		t.Fatal("a second call got through while a probe was in flight; that is a stampede onto a recovering dependency")
	}
	close(finish)
}

func TestStateChangeIsReported(t *testing.T) {
	var transitions []string
	b := breaker.New(breaker.Options{
		Threshold: 1, Cooldown: time.Minute,
		OnStateChange: func(from, to breaker.State) {
			transitions = append(transitions, string(from)+"->"+string(to))
		},
	})
	_ = b.Do(context.Background(), fail)

	if len(transitions) != 1 || transitions[0] != "closed->open" {
		t.Fatalf("transitions = %v", transitions)
	}
}
