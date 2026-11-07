// Package greeter is the greeter domain.
package greeter

import (
	"errors"

	"github.com/IncepTools/go/examples/minimal/apps/greeter/hello"
	"github.com/IncepTools/go/src/core/di"
	"github.com/IncepTools/go/src/core/module"
)

// App is the greeter domain.
type App struct {
	module.BaseApp
}

// Descriptor is how a service mounts this domain: register every provider
// first, then build.
//
// The two phases are a correctness requirement rather than ceremony.
// Building a module constructs its controller, which resolves its service out
// of the container — so every provider in the whole application must be
// registered before any module is built.
func Descriptor() module.Descriptor {
	return module.Descriptor{
		Providers: Providers,
		Build:     func(s *di.Scope) module.App { return NewApp(s) },
	}
}

// Providers registers everything this domain's modules resolve.
func Providers(c *di.Container) error {
	return errors.Join(
		hello.Providers(c),
	)
}

// NewApp wires the domain's modules. Every provider is registered by the time
// this runs.
func NewApp(s *di.Scope) *App {
	return &App{BaseApp: module.NewApp("greeter", hello.New(s))}
}

var _ module.App = (*App)(nil)
