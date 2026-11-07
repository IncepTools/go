package lock_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/IncepTools/go/src/platform/lock"
)

func TestSecondAcquireIsRefused(t *testing.T) {
	l := lock.NewMemory()
	ctx := context.Background()

	first, err := l.Acquire(ctx, "job:sweep", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Acquire(ctx, "job:sweep", time.Minute); !errors.Is(err, lock.ErrNotAcquired) {
		t.Fatalf("err = %v, want ErrNotAcquired", err)
	}

	if err := first.Release(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Acquire(ctx, "job:sweep", time.Minute); err != nil {
		t.Fatalf("the lock was not released: %v", err)
	}
}

func TestReleaseOnlyReleasesYourOwnLock(t *testing.T) {
	// The failure this guards: holder A's TTL expires mid-work, B acquires,
	// then A finishes and calls Release — deleting B's lock and putting two
	// workers inside the critical section.
	l := lock.NewMemory()
	ctx := context.Background()

	a, err := l.Acquire(ctx, "k", 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)

	b, err := l.Acquire(ctx, "k", time.Minute)
	if err != nil {
		t.Fatalf("the expired lock was not re-acquirable: %v", err)
	}

	if err := a.Release(ctx); !errors.Is(err, lock.ErrNotHeld) {
		t.Fatalf("the expired holder's release returned %v, want ErrNotHeld", err)
	}
	if _, err := l.Acquire(ctx, "k", time.Minute); !errors.Is(err, lock.ErrNotAcquired) {
		t.Fatal("the expired holder's release deleted the new holder's lock")
	}
	_ = b.Release(ctx)
}

func TestRefreshExtendsTheTTL(t *testing.T) {
	l := lock.NewMemory()
	ctx := context.Background()

	held, err := l.Acquire(ctx, "k", 30*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if err := held.Refresh(ctx, time.Minute); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)

	if _, err := l.Acquire(ctx, "k", time.Minute); !errors.Is(err, lock.ErrNotAcquired) {
		t.Fatal("the refreshed lock expired anyway")
	}
}

func TestDoSkipsWhenHeldElsewhere(t *testing.T) {
	l := lock.NewMemory()
	ctx := context.Background()

	held, err := l.Acquire(ctx, "job", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release(ctx) //nolint:errcheck

	var ran bool
	did, err := lock.Do(ctx, l, "job", time.Minute, func(context.Context) error {
		ran = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if did || ran {
		t.Fatal("the work ran although another holder had the lock")
	}
}

func TestDoReleasesEvenWhenTheContextIsCancelled(t *testing.T) {
	l := lock.NewMemory()
	ctx, cancel := context.WithCancel(context.Background())

	_, err := lock.Do(ctx, l, "job", time.Minute, func(context.Context) error {
		cancel()
		return context.Canceled
	})
	if err == nil {
		t.Fatal("expected the work's error")
	}

	if _, err := l.Acquire(context.Background(), "job", time.Minute); err != nil {
		t.Fatalf("the lock was not released after a cancelled run: %v", err)
	}
}
