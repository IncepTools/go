package lifecycle_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/IncepTools/go/src/core/lifecycle"
)

func TestRunUnwindsWhenAReadyHookFails(t *testing.T) {
	// The leak this guards: a ready hook that fails used to make Run return
	// immediately, leaving the listener accepting traffic and every
	// already-initialised hook unshut. The process looked alive and served
	// requests against half-built dependencies.
	var mu sync.Mutex
	var order []string
	record := func(step string) {
		mu.Lock()
		order = append(order, step)
		mu.Unlock()
	}

	boom := errors.New("cache is unreachable")
	r := &lifecycle.Registry{}
	r.Add(lifecycle.Hook{
		Name:     "database",
		Init:     func(context.Context) error { record("db.init"); return nil },
		Shutdown: func(context.Context) error { record("db.shutdown"); return nil },
	})
	r.Add(lifecycle.Hook{
		Name:  "cache",
		Ready: func(context.Context) error { record("cache.ready"); return boom },
	})

	serving := make(chan struct{})
	release := make(chan struct{})
	serve := func() error {
		close(serving)
		<-release
		return nil
	}
	stop := func(context.Context) error {
		record("listener.stop")
		close(release)
		return nil
	}

	err := lifecycle.Run(context.Background(), r, serve, stop, time.Second)
	<-serving

	if !errors.Is(err, boom) {
		t.Fatalf("Run error = %v, want the ready failure", err)
	}

	mu.Lock()
	defer mu.Unlock()
	got := order
	want := []string{"db.init", "cache.ready", "listener.stop", "db.shutdown"}
	if len(got) != len(want) {
		t.Fatalf("steps = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("steps = %v, want %v", got, want)
		}
	}
}

func TestRunShutsDownWhatInitialisedBeforeAFailure(t *testing.T) {
	boom := errors.New("migrations failed")
	var shutdowns []string
	var mu sync.Mutex

	r := &lifecycle.Registry{}
	r.Add(lifecycle.Hook{
		Name: "database",
		Init: func(context.Context) error { return nil },
		Shutdown: func(context.Context) error {
			mu.Lock()
			shutdowns = append(shutdowns, "database")
			mu.Unlock()
			return nil
		},
	})
	r.Add(lifecycle.Hook{
		Name: "migrator",
		Init: func(context.Context) error { return boom },
	})

	served := false
	err := lifecycle.Run(context.Background(), r,
		func() error { served = true; return nil },
		func(context.Context) error { return nil },
		time.Second)

	if !errors.Is(err, boom) {
		t.Fatalf("Run error = %v, want the init failure", err)
	}
	// Nothing should start listening when boot failed.
	if served {
		t.Error("the listener was started despite a failed init")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(shutdowns) != 1 || shutdowns[0] != "database" {
		t.Fatalf("shutdowns = %v, want the already-initialised hook unwound", shutdowns)
	}
}

func TestRunUsesAFreshContextForDraining(t *testing.T) {
	// Deriving the drain context from the cancelled parent is the classic
	// bug: every shutdown hook receives a context that is already done and
	// skips its work, so nothing actually drains.
	var drainErr error
	var mu sync.Mutex

	r := &lifecycle.Registry{}
	r.Add(lifecycle.Hook{
		Name: "worker",
		Init: func(context.Context) error { return nil },
		Shutdown: func(ctx context.Context) error {
			mu.Lock()
			drainErr = ctx.Err()
			mu.Unlock()
			return nil
		},
	})
	r.Add(lifecycle.Hook{
		Name:  "failing",
		Ready: func(context.Context) error { return errors.New("nope") },
	})

	release := make(chan struct{})
	_ = lifecycle.Run(context.Background(), r,
		func() error { <-release; return nil },
		func(context.Context) error { close(release); return nil },
		time.Second)

	mu.Lock()
	defer mu.Unlock()
	if drainErr != nil {
		t.Fatalf("the shutdown hook ran on an expired context: %v", drainErr)
	}
}
