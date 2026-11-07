package di_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/IncepTools/go/src/core/di"
)

type repo struct{ dsn string }
type svc struct{ r *repo }

func TestResolveConstructsOnceAndLazily(t *testing.T) {
	c := di.New()
	var built int
	di.MustProvide(c, func(*di.Scope) (*repo, error) {
		built++
		return &repo{dsn: "x"}, nil
	})

	if built != 0 {
		t.Fatalf("provider ran at registration; it must be lazy")
	}

	a := di.MustResolve[*repo](c)
	b := di.MustResolve[*repo](c)
	if a != b {
		t.Fatal("two resolves returned different instances; a provider is a singleton")
	}
	if built != 1 {
		t.Fatalf("constructor ran %d times, want 1", built)
	}
}

func TestResolveIsConcurrencySafe(t *testing.T) {
	c := di.New()
	var mu sync.Mutex
	built := 0
	di.MustProvide(c, func(*di.Scope) (*repo, error) {
		mu.Lock()
		built++
		mu.Unlock()
		return &repo{}, nil
	})

	var wg sync.WaitGroup
	results := make([]*repo, 50)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = di.MustResolve[*repo](c)
		}(i)
	}
	wg.Wait()

	if built != 1 {
		t.Fatalf("constructor ran %d times under concurrency, want 1", built)
	}
	for i := 1; i < len(results); i++ {
		if results[i] != results[0] {
			t.Fatal("concurrent resolves produced different instances")
		}
	}
}

func TestNestedResolve(t *testing.T) {
	c := di.New()
	di.MustProvide(c, func(*di.Scope) (*repo, error) { return &repo{dsn: "db"}, nil })
	di.MustProvide(c, func(s *di.Scope) (*svc, error) {
		return &svc{r: di.MustResolve[*repo](s)}, nil
	})

	got := di.MustResolve[*svc](c)
	if got.r.dsn != "db" {
		t.Fatalf("nested dependency not injected: %+v", got)
	}
}

func TestCycleIsReportedNotDeadlocked(t *testing.T) {
	type a struct{}
	type b struct{}

	c := di.New()
	di.MustProvide(c, func(s *di.Scope) (*a, error) {
		_, err := di.Resolve[*b](s)
		return &a{}, err
	})
	di.MustProvide(c, func(s *di.Scope) (*b, error) {
		_, err := di.Resolve[*a](s)
		return &b{}, err
	})

	done := make(chan error, 1)
	go func() {
		_, err := di.Resolve[*a](c)
		done <- err
	}()

	err := <-done
	if err == nil {
		t.Fatal("a dependency cycle resolved without error")
	}
	if !errors.Is(err, di.ErrCycle) {
		t.Fatalf("error = %v, want a cycle error", err)
	}
	if !strings.Contains(err.Error(), "->") {
		t.Fatalf("cycle error does not name the path: %v", err)
	}
}

func TestDuplicateProviderIsRefused(t *testing.T) {
	c := di.New()
	if err := di.Provide(c, func(*di.Scope) (*repo, error) { return &repo{}, nil }); err != nil {
		t.Fatal(err)
	}
	err := di.Provide(c, func(*di.Scope) (*repo, error) { return &repo{}, nil })
	if err == nil {
		t.Fatal("a second provider for the same type was accepted; it would silently shadow the first")
	}
}

func TestNamedBindings(t *testing.T) {
	c := di.New()
	if err := di.SupplyNamed(c, "primary", &repo{dsn: "primary"}); err != nil {
		t.Fatal(err)
	}
	if err := di.SupplyNamed(c, "replica", &repo{dsn: "replica"}); err != nil {
		t.Fatal(err)
	}
	if got := di.MustResolveNamed[*repo](c, "replica").dsn; got != "replica" {
		t.Fatalf("dsn = %q, want replica", got)
	}
}

func TestMissingProviderNamesTheType(t *testing.T) {
	c := di.New()
	_, err := di.Resolve[*repo](c)
	if !errors.Is(err, di.ErrNotProvided) {
		t.Fatalf("err = %v, want ErrNotProvided", err)
	}
	if !strings.Contains(err.Error(), "repo") {
		t.Fatalf("error does not name the missing type: %v", err)
	}
}

type closer struct {
	name string
	log  *[]string
}

func (c *closer) Close(context.Context) error {
	*c.log = append(*c.log, c.name)
	return nil
}

func TestCloseRunsInReverseConstructionOrder(t *testing.T) {
	var log []string
	c := di.New()
	di.MustProvide(c, func(*di.Scope) (*closer, error) { return &closer{name: "first", log: &log}, nil })
	di.MustProvideNamed(c, "second", func(s *di.Scope) (*closer, error) {
		di.MustResolve[*closer](s) // force "first" to construct first
		return &closer{name: "second", log: &log}, nil
	})

	di.MustResolveNamed[*closer](c, "second")
	if err := c.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(log) != 2 || log[0] != "second" || log[1] != "first" {
		t.Fatalf("close order = %v, want [second first]", log)
	}
}

func TestEagerSurfacesConstructorFailures(t *testing.T) {
	c := di.New()
	di.MustProvide(c, func(*di.Scope) (*repo, error) { return nil, errors.New("bad dsn") })

	if err := c.Eager(); err == nil {
		t.Fatal("Eager did not report the failing provider; a bad dependency must fail the boot")
	}
}
