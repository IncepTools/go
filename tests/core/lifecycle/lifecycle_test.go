package lifecycle_test

import (
	"context"
	"errors"
	"testing"

	"github.com/IncepTools/go/src/core/lifecycle"
)

func hook(name string, log *[]string, initErr error) lifecycle.Hook {
	return lifecycle.Hook{
		Name: name,
		Init: func(context.Context) error {
			*log = append(*log, "init:"+name)
			return initErr
		},
		Shutdown: func(context.Context) error {
			*log = append(*log, "down:"+name)
			return nil
		},
	}
}

func TestShutdownRunsInReverseOrder(t *testing.T) {
	var log []string
	r := &lifecycle.Registry{}
	r.Add(hook("db", &log, nil))
	r.Add(hook("cache", &log, nil))
	r.Add(hook("worker", &log, nil))

	ctx := context.Background()
	if err := r.Init(ctx); err != nil {
		t.Fatal(err)
	}
	if err := r.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}

	want := []string{"init:db", "init:cache", "init:worker", "down:worker", "down:cache", "down:db"}
	if len(log) != len(want) {
		t.Fatalf("log = %v, want %v", log, want)
	}
	for i := range want {
		if log[i] != want[i] {
			t.Fatalf("log = %v, want %v", log, want)
		}
	}
}

func TestInitStopsAtFirstFailureAndUnwindsOnlyWhatStarted(t *testing.T) {
	var log []string
	r := &lifecycle.Registry{}
	r.Add(hook("db", &log, nil))
	r.Add(hook("broken", &log, errors.New("no")))
	r.Add(hook("never", &log, nil))

	ctx := context.Background()
	err := r.Init(ctx)
	if err == nil {
		t.Fatal("Init did not report the failure")
	}
	for _, l := range log {
		if l == "init:never" {
			t.Fatal("a hook after the failing one still ran")
		}
	}

	if err := r.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	for _, l := range log {
		if l == "down:broken" || l == "down:never" {
			t.Fatalf("shut down something that never initialised: %v", log)
		}
	}
	if log[len(log)-1] != "down:db" {
		t.Fatalf("db was not unwound: %v", log)
	}
}

type observed struct{ ran map[string]bool }

func (o *observed) OnInit(context.Context) error     { o.ran["init"] = true; return nil }
func (o *observed) OnShutdown(context.Context) error { o.ran["down"] = true; return nil }

func TestObserveDetectsImplementedPhases(t *testing.T) {
	o := &observed{ran: map[string]bool{}}
	r := &lifecycle.Registry{}
	r.Observe("thing", o)
	r.Observe("nothing", struct{}{})

	if names := r.Names(); len(names) != 1 || names[0] != "thing" {
		t.Fatalf("names = %v; a value implementing no phase must not be registered", names)
	}

	ctx := context.Background()
	_ = r.Init(ctx)
	_ = r.Ready(ctx)
	_ = r.Shutdown(ctx)

	if !o.ran["init"] || !o.ran["down"] {
		t.Fatalf("phases ran = %v", o.ran)
	}
}

func TestShutdownGetsALiveContextAfterCancellation(t *testing.T) {
	// The bug this guards: deriving the drain context from the cancelled one,
	// so every shutdown hook sees a context that is already done and skips
	// its work.
	var sawCancelled bool
	r := &lifecycle.Registry{}
	r.Add(lifecycle.Hook{
		Name: "drain",
		Shutdown: func(ctx context.Context) error {
			sawCancelled = ctx.Err() != nil
			return nil
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = r.Init(ctx)

	dctx, dcancel := context.WithTimeout(context.WithoutCancel(ctx), 0)
	dcancel()
	_ = dctx

	freshCtx := context.WithoutCancel(ctx)
	if err := r.Shutdown(freshCtx); err != nil {
		t.Fatal(err)
	}
	if sawCancelled {
		t.Fatal("the shutdown hook was handed an already-cancelled context")
	}
}
