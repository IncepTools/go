package di_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/IncepTools/go/src/core/di"
)

type database struct{ dsn string }

type productRepo struct{ db *database }

type productService struct{ repo *productRepo }

func (s *productService) Describe() string { return "service on " + s.repo.db.dsn }

// A constructor asks for what it needs, in code. Nothing is resolved by
// reflection or by struct tag.
func Example() {
	c := di.New()

	di.MustSupply(c, &database{dsn: "postgres://db/shop"})

	di.MustProvide(c, func(s *di.Scope) (*productRepo, error) {
		return &productRepo{db: di.MustResolve[*database](s)}, nil
	})
	di.MustProvide(c, func(s *di.Scope) (*productService, error) {
		return &productService{repo: di.MustResolve[*productRepo](s)}, nil
	})

	fmt.Println(di.MustResolve[*productService](c).Describe())
	// Output: service on postgres://db/shop
}

// Providers are lazy and run exactly once, however many times the value is
// resolved.
func ExampleProvide_constructsOnce() {
	c := di.New()
	built := 0

	di.MustProvide(c, func(*di.Scope) (*database, error) {
		built++
		return &database{dsn: "x"}, nil
	})

	fmt.Println("before any resolve:", built)
	_ = di.MustResolve[*database](c)
	_ = di.MustResolve[*database](c)
	fmt.Println("after two resolves:", built)
	// Output:
	// before any resolve: 0
	// after two resolves: 1
}

// A cycle is reported with the path that caused it, rather than
// deadlocking or overflowing the stack.
func ExampleResolve_cycle() {
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

	_, err := di.Resolve[*a](c)
	fmt.Println(errors.Is(err, di.ErrCycle))
	// Output: true
}

// Register an implementation under the interface its consumers ask for.
// Without the explicit type parameter the binding would be registered
// under the concrete type and nothing asking for the interface would find
// it.
func ExampleSupply_interface() {
	type Clock interface{ Now() string }

	c := di.New()
	di.MustSupply[Clock](c, fixedClock{})

	fmt.Println(di.MustResolve[Clock](c).Now())
	// Output: 2026-01-01T00:00:00Z
}

type fixedClock struct{}

func (fixedClock) Now() string { return "2026-01-01T00:00:00Z" }

// Eager constructs everything before the listener opens, so a bad
// dependency fails the deploy rather than the first request that needs it.
func ExampleContainer_Eager() {
	c := di.New()
	di.MustProvide(c, func(*di.Scope) (*database, error) {
		return nil, errors.New("could not parse the DSN")
	})

	fmt.Println(c.Eager() != nil)
	_ = c.Close(context.Background())
	// Output: true
}
