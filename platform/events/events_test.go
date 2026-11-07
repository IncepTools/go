package events_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/IncepTools/go/core/ctxkit"
	"github.com/IncepTools/go/platform/events"
)

type created struct{ ID string }
type deleted struct{ ID string }

func TestSyncHandlerErrorsReachThePublisher(t *testing.T) {
	b := events.New()
	events.On(b, "fails", events.Sync, func(context.Context, created) error {
		return errors.New("nope")
	})

	if err := events.Publish(context.Background(), b, created{ID: "1"}); err == nil {
		t.Fatal("a sync handler's error did not reach Publish")
	}
}

func TestAsyncHandlerErrorsDoNotReachThePublisher(t *testing.T) {
	b := events.New()
	events.On(b, "fails", events.Async, func(context.Context, created) error {
		return errors.New("nope")
	})

	if err := events.Publish(context.Background(), b, created{ID: "1"}); err != nil {
		t.Fatalf("an async handler's failure must not fail the publisher: %v", err)
	}
	b.Wait()
}

func TestHandlersOnlyReceiveTheirOwnType(t *testing.T) {
	b := events.New()
	var got []string
	var mu sync.Mutex

	events.On(b, "on-created", events.Sync, func(_ context.Context, e created) error {
		mu.Lock()
		got = append(got, "created:"+e.ID)
		mu.Unlock()
		return nil
	})
	events.On(b, "on-deleted", events.Sync, func(_ context.Context, e deleted) error {
		mu.Lock()
		got = append(got, "deleted:"+e.ID)
		mu.Unlock()
		return nil
	})

	_ = events.Publish(context.Background(), b, created{ID: "a"})
	if len(got) != 1 || got[0] != "created:a" {
		t.Fatalf("got = %v; a handler received an event of the wrong type", got)
	}
}

func TestPanickingHandlerDoesNotTakeDownThePublisher(t *testing.T) {
	b := events.New()
	events.On(b, "boom", events.Sync, func(context.Context, created) error {
		panic("handler is broken")
	})

	err := events.Publish(context.Background(), b, created{ID: "1"})
	if err == nil {
		t.Fatal("a panicking handler reported success")
	}
}

func TestAsyncHandlerKeepsTraceAndSurvivesCancellation(t *testing.T) {
	b := events.New()
	done := make(chan string, 1)

	events.On(b, "slow", events.Async, func(ctx context.Context, e created) error {
		// The publisher's context is cancelled immediately below. A handler
		// given that context would see Err() != nil and abandon the work.
		time.Sleep(10 * time.Millisecond)
		if ctx.Err() != nil {
			done <- "cancelled"
			return nil
		}
		done <- ctxkit.Trace(ctx)
		return nil
	})

	ctx, cancel := context.WithCancel(ctxkit.WithTrace(context.Background(), "trace-123"))
	_ = events.Publish(ctx, b, created{ID: "1"})
	cancel()

	select {
	case got := <-done:
		if got == "cancelled" {
			t.Fatal("the async handler inherited the publisher's cancellation")
		}
		if got != "trace-123" {
			t.Fatalf("trace = %q, want trace-123 to be carried through", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler never ran")
	}
}

func TestWaitDrainsInFlightHandlers(t *testing.T) {
	b := events.New()
	var ran int
	var mu sync.Mutex
	events.On(b, "slow", events.Async, func(context.Context, created) error {
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		ran++
		mu.Unlock()
		return nil
	})

	for range 5 {
		_ = events.Publish(context.Background(), b, created{})
	}
	b.Wait()

	mu.Lock()
	defer mu.Unlock()
	if ran != 5 {
		t.Fatalf("ran = %d, want 5; Wait must drain before shutdown", ran)
	}
}

func TestPublishWithNoSubscribersIsFine(t *testing.T) {
	if err := events.Publish(context.Background(), events.New(), created{}); err != nil {
		t.Fatal(err)
	}
}
