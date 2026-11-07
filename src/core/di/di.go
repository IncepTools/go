// Package di is the dependency-injection container: typed, lazy providers
// resolved through Go generics, with no reflection and no struct tags.
//
// What it gives you over wiring by hand in a bootstrap function:
//
//   - Providers are LAZY. A dependency is constructed the first time something
//     asks for it, exactly once, even under concurrent resolution.
//   - Cycles are REPORTED, with the full path, instead of deadlocking or
//     overflowing the stack.
//   - Teardown is ORDERED. Anything that implements Closer is closed in the
//     reverse of its construction order, so a pool never outlives the thing
//     draining into it.
//
// What it deliberately does not do: reflection-based autowiring by struct tag.
// A constructor asks for what it needs, in code, and a missing dependency is a
// boot-time error naming both sides. Magic that fails at runtime is worse than
// a line of wiring.
//
//	di.Provide(c, func(s *di.Scope) (*Repo, error) {
//	    return NewRepo(di.MustResolve[*gorm.DB](s)), nil
//	})
//	repo := di.MustResolve[*Repo](c)
package di

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
)

// Closer is implemented by a provider whose instance holds something that must
// be released: a pool, a client, a file, a background loop. Container.Close
// calls it in reverse construction order.
//
// A plain io.Closer is honoured too.
type Closer interface {
	Close(ctx context.Context) error
}

// Constructor builds one instance. It receives a *Scope rather than the
// container so that resolving a dependency from inside it extends the
// resolution path, which is what makes cycle reporting possible.
type Constructor[T any] func(*Scope) (T, error)

// Resolver is satisfied by *Container and by the *Scope handed to a
// constructor. It cannot be implemented outside this package.
type Resolver interface {
	scope() *Scope
}

type key struct {
	typ  reflect.Type
	name string
}

func (k key) String() string {
	if k.name == "" {
		return k.typ.String()
	}
	return k.typ.String() + `["` + k.name + `"]`
}

type entry struct {
	mu    sync.Mutex
	built bool
	val   any
	err   error
	ctor  func(*Scope) (any, error)
	key   key
}

// Container holds providers and the instances built from them.
//
// The zero value is not usable; call New.
type Container struct {
	mu       sync.RWMutex
	entries  map[key]*entry
	built    []*entry // construction order, for reverse teardown
	closed   bool
	closeErr error
}

// New returns an empty container.
func New() *Container {
	return &Container{entries: make(map[key]*entry)}
}

func (c *Container) scope() *Scope { return &Scope{c: c} }

// Scope is a resolution in progress. It carries the chain of types being
// constructed so that a cycle can be reported as a path rather than a hang.
type Scope struct {
	c     *Container
	chain []key
}

func (s *Scope) scope() *Scope { return s }

// Container returns the container this scope resolves against.
func (s *Scope) Container() *Container { return s.c }

func (s *Scope) descend(k key) *Scope {
	return &Scope{c: s.c, chain: append(append([]key(nil), s.chain...), k)}
}

func (s *Scope) cycle(k key) bool {
	for _, c := range s.chain {
		if c == k {
			return true
		}
	}
	return false
}

func (s *Scope) pathString(k key) string {
	parts := make([]string, 0, len(s.chain)+1)
	for _, c := range s.chain {
		parts = append(parts, c.String())
	}
	return strings.Join(append(parts, k.String()), " -> ")
}

// ErrNotProvided is returned when nothing provides the requested type.
var ErrNotProvided = errors.New("di: no provider registered")

// ErrCycle is returned when providers depend on each other in a loop.
var ErrCycle = errors.New("di: dependency cycle")

// ErrClosed is returned by a resolve or a provide after Close.
var ErrClosed = errors.New("di: container is closed")

func typeOf[T any]() reflect.Type { return reflect.TypeOf((*T)(nil)).Elem() }

// Provide registers a lazy singleton constructor for T.
//
// Registering the same T twice is an error rather than a silent overwrite: two
// providers for one type means two different objects depending on resolution
// order, which is a bug that only shows up in production. Use a name when you
// genuinely want two (a primary and a replica pool, say).
func Provide[T any](c *Container, ctor Constructor[T]) error {
	return ProvideNamed(c, "", ctor)
}

// ProvideNamed registers a lazy singleton for T under a name, so one type can
// have several distinct bindings.
func ProvideNamed[T any](c *Container, name string, ctor Constructor[T]) error {
	if ctor == nil {
		return fmt.Errorf("di: nil constructor for %s", typeOf[T]())
	}
	k := key{typ: typeOf[T](), name: name}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrClosed
	}
	if _, dup := c.entries[k]; dup {
		return fmt.Errorf("di: %s is already provided; use ProvideNamed for a second binding", k)
	}
	c.entries[k] = &entry{
		key: k,
		ctor: func(s *Scope) (any, error) {
			v, err := ctor(s)
			return v, err
		},
	}
	return nil
}

// MustProvide is Provide, panicking on a duplicate registration. Registration
// happens at boot from code you wrote, so a panic there is a compile-time-ish
// failure, not a runtime risk.
func MustProvide[T any](c *Container, ctor Constructor[T]) {
	if err := Provide(c, ctor); err != nil {
		panic(err)
	}
}

// Supply registers an already-constructed value. Use it for things built
// before the container exists — the parsed config, the logger, a *gorm.DB the
// caller opened.
func Supply[T any](c *Container, v T) error {
	return SupplyNamed(c, "", v)
}

// SupplyNamed registers an already-constructed value under a name.
func SupplyNamed[T any](c *Container, name string, v T) error {
	return ProvideNamed(c, name, func(*Scope) (T, error) { return v, nil })
}

// MustSupply is Supply, panicking on a duplicate registration.
func MustSupply[T any](c *Container, v T) {
	if err := Supply(c, v); err != nil {
		panic(err)
	}
}

// Resolve returns the instance of T, constructing it if this is the first ask.
func Resolve[T any](r Resolver) (T, error) { return ResolveNamed[T](r, "") }

// ResolveNamed returns the named instance of T.
func ResolveNamed[T any](r Resolver, name string) (T, error) {
	var zero T
	s := r.scope()
	k := key{typ: typeOf[T](), name: name}

	if s.cycle(k) {
		return zero, fmt.Errorf("%w: %s", ErrCycle, s.pathString(k))
	}

	s.c.mu.RLock()
	e, ok := s.c.entries[k]
	closed := s.c.closed
	s.c.mu.RUnlock()
	if closed {
		return zero, ErrClosed
	}
	if !ok {
		return zero, fmt.Errorf("%w for %s (asked for by: %s)", ErrNotProvided, k, s.pathString(k))
	}

	// Per-entry lock, not the container lock: a constructor resolves its own
	// dependencies, and holding the container lock across that would deadlock
	// on the very first nested resolve.
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.built {
		if e.err != nil {
			return zero, e.err
		}
		v, _ := e.val.(T)
		return v, nil
	}

	val, err := e.ctor(s.descend(k))
	e.built, e.val, e.err = true, val, err
	if err != nil {
		e.err = fmt.Errorf("di: constructing %s: %w", k, err)
		return zero, e.err
	}

	s.c.mu.Lock()
	s.c.built = append(s.c.built, e)
	s.c.mu.Unlock()

	v, _ := val.(T)
	return v, nil
}

// MustResolve is Resolve, panicking on failure.
//
// It is the right call inside a constructor and inside module wiring, which
// run at boot: a missing provider there should stop the process, loudly, not
// be handled. It is the wrong call in a request path.
func MustResolve[T any](r Resolver) T {
	v, err := Resolve[T](r)
	if err != nil {
		panic(err)
	}
	return v
}

// MustResolveNamed is ResolveNamed, panicking on failure.
func MustResolveNamed[T any](r Resolver, name string) T {
	v, err := ResolveNamed[T](r, name)
	if err != nil {
		panic(err)
	}
	return v
}

// Has reports whether T is provided, without constructing it.
func Has[T any](c *Container) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.entries[key{typ: typeOf[T]()}]
	return ok
}

// NewScope returns a resolution scope for c. Use it where a *Scope is
// required outside a constructor — module construction, boot-time wiring.
func NewScope(c *Container) *Scope { return c.scope() }

// Invoke runs fn with a resolution scope. Use it for boot-time work that needs
// several dependencies but produces nothing worth registering.
func Invoke(c *Container, fn func(*Scope) error) error { return fn(c.scope()) }

// Eager constructs every registered provider now, instead of on first use.
//
// Call it at the end of boot. A provider that fails — a bad DSN, an
// unreachable broker — then fails the deploy rather than the first request
// that happens to need it.
func (c *Container) Eager() error {
	c.mu.RLock()
	keys := make([]key, 0, len(c.entries))
	for k := range c.entries {
		keys = append(keys, k)
	}
	c.mu.RUnlock()

	var errs []error
	for _, k := range keys {
		c.mu.RLock()
		e := c.entries[k]
		c.mu.RUnlock()
		e.mu.Lock()
		if !e.built {
			val, err := e.ctor(&Scope{c: c, chain: []key{k}})
			e.built, e.val, e.err = true, val, err
			if err == nil {
				c.mu.Lock()
				c.built = append(c.built, e)
				c.mu.Unlock()
			}
		}
		if e.err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", k, e.err))
		}
		e.mu.Unlock()
	}
	return errors.Join(errs...)
}

// Providers lists every registered binding, for diagnostics.
func (c *Container) Providers() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]string, 0, len(c.entries))
	for k := range c.entries {
		out = append(out, k.String())
	}
	return out
}

// Close releases every constructed instance that implements Closer or
// io.Closer, in the reverse of construction order, and marks the container
// unusable. It is safe to call more than once; later calls return the first
// result.
func (c *Container) Close(ctx context.Context) error {
	c.mu.Lock()
	if c.closed {
		err := c.closeErr
		c.mu.Unlock()
		return err
	}
	c.closed = true
	built := append([]*entry(nil), c.built...)
	c.mu.Unlock()

	var errs []error
	for i := len(built) - 1; i >= 0; i-- {
		e := built[i]
		switch v := e.val.(type) {
		case Closer:
			if err := v.Close(ctx); err != nil {
				errs = append(errs, fmt.Errorf("closing %s: %w", e.key, err))
			}
		case io.Closer:
			if err := v.Close(); err != nil {
				errs = append(errs, fmt.Errorf("closing %s: %w", e.key, err))
			}
		}
	}
	c.mu.Lock()
	c.closeErr = errors.Join(errs...)
	err := c.closeErr
	c.mu.Unlock()
	return err
}

// MustProvideNamed is ProvideNamed, panicking on a duplicate registration.
func MustProvideNamed[T any](c *Container, name string, ctor Constructor[T]) {
	if err := ProvideNamed(c, name, ctor); err != nil {
		panic(err)
	}
}
