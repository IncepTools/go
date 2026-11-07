package retry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/IncepTools/go/src/core/errs"
	"github.com/IncepTools/go/src/reliability/retry"
)

func fast() retry.Policy {
	return retry.Policy{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 2 * time.Millisecond, NoJitter: true}
}

func TestRetriesUntilSuccess(t *testing.T) {
	calls := 0
	err := retry.Do(context.Background(), fast(), func(context.Context) error {
		calls++
		if calls < 3 {
			return errs.Unavailable("down")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestDoesNotRetryAClientError(t *testing.T) {
	calls := 0
	err := retry.Do(context.Background(), fast(), func(context.Context) error {
		calls++
		return errs.Invalid("bad cidr")
	})
	if err == nil {
		t.Fatal("expected the error")
	}
	if calls != 1 {
		t.Fatalf("calls = %d; a 400 will fail identically every time", calls)
	}
}

func TestDoesNotRetryAnUnclassifiedError(t *testing.T) {
	calls := 0
	_ = retry.Do(context.Background(), fast(), func(context.Context) error {
		calls++
		return errors.New("something happened")
	})
	if calls != 1 {
		t.Fatalf("calls = %d; an error nobody classified must not be assumed safe to repeat", calls)
	}
}

func TestRetriesAConcurrentModificationConflictOnly(t *testing.T) {
	calls := 0
	_ = retry.Do(context.Background(), fast(), func(context.Context) error {
		calls++
		return errs.Conflict("raced").WithCode("CONCURRENT_MODIFICATION")
	})
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}

	calls = 0
	_ = retry.Do(context.Background(), fast(), func(context.Context) error {
		calls++
		return errs.Conflict("taken").WithCode("NAME_TAKEN")
	})
	if calls != 1 {
		t.Fatalf("calls = %d; a uniqueness conflict is permanent", calls)
	}
}

func TestStopsWhenTheContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	err := retry.Do(ctx, retry.Policy{MaxAttempts: 5, BaseDelay: 50 * time.Millisecond, NoJitter: true},
		func(context.Context) error {
			calls++
			cancel()
			return errs.Unavailable("down")
		})
	if err == nil {
		t.Fatal("expected an error")
	}
	if calls != 1 {
		t.Fatalf("calls = %d; a cancelled caller must not sleep out the backoff", calls)
	}
}

func TestValueReturnsTheResult(t *testing.T) {
	calls := 0
	got, err := retry.Value(context.Background(), fast(), func(context.Context) (string, error) {
		calls++
		if calls < 2 {
			return "", errs.Timeout("slow")
		}
		return "ok", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "ok" {
		t.Fatalf("got = %q", got)
	}
}
