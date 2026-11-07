package errs_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/IncepTools/go/src/core/errs"
)

func TestKindRoundTrip(t *testing.T) {
	cases := []struct {
		err  *errs.Error
		kind errs.Kind
	}{
		{errs.Invalid("x"), errs.KindInvalid},
		{errs.NotFound("x"), errs.KindNotFound},
		{errs.Conflict("x"), errs.KindConflict},
		{errs.Exhausted("x"), errs.KindExhausted},
		{errs.Unavailable("x"), errs.KindUnavailable},
	}
	for _, c := range cases {
		if got := errs.KindOf(c.err); got != c.kind {
			t.Errorf("KindOf(%v) = %q, want %q", c.err, got, c.kind)
		}
	}
}

func TestUnclassifiedErrorBecomesInternal(t *testing.T) {
	plain := errors.New("pq: connection refused to 10.0.0.4:5432")
	e := errs.From(plain)

	if errs.KindOf(e) != errs.KindInternal {
		t.Fatalf("kind = %q, want internal", errs.KindOf(e))
	}
	if e.Message == plain.Error() {
		t.Fatal("the driver message became the public message; it must be replaced and only logged")
	}
	if !errors.Is(e, plain) {
		t.Fatal("the original error was lost; it must stay reachable for the log")
	}
}

func TestWrapOfNilIsNil(t *testing.T) {
	if e := errs.Wrap(errs.KindInternal, "x", nil); e != nil {
		t.Fatalf("wrapping nil produced %v", e)
	}
}

func TestBuildersDoNotMutateTheReceiver(t *testing.T) {
	base := errs.NotFound("product not found")
	withCode := base.WithCode("NOT_FOUND")

	if base.Code != "" {
		t.Fatal("WithCode mutated the original; the builders must copy")
	}
	if withCode.Code != "NOT_FOUND" {
		t.Fatalf("code = %q", withCode.Code)
	}
}

func TestDetailsAccumulate(t *testing.T) {
	e := errs.Invalid("validation failed").
		WithDetails(errs.Detail{Field: "name"}).
		WithDetails(errs.Detail{Field: "cidr"})
	if len(e.Details) != 2 {
		t.Fatalf("details = %d, want 2", len(e.Details))
	}
}

func TestErrorStringIncludesCause(t *testing.T) {
	cause := fmt.Errorf("23505 duplicate key")
	e := errs.Conflict("already exists").WithCause(cause)
	if got := e.Error(); got == "already exists" {
		t.Fatalf("Error() = %q; it should include the cause for the log", got)
	}
}

func TestCodeOfUnwrapsThroughStandardWrapping(t *testing.T) {
	inner := errs.Conflict("taken").WithCode("NAME_TAKEN")
	outer := fmt.Errorf("creating product: %w", inner)

	if got := errs.CodeOf(outer); got != "NAME_TAKEN" {
		t.Fatalf("CodeOf = %q, want NAME_TAKEN", got)
	}
	if !errs.Is(outer, errs.KindConflict) {
		t.Fatal("kind was lost through fmt.Errorf wrapping")
	}
}
