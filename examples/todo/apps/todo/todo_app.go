// Package todo is the todo domain.
package todo

import (
	"errors"

	"github.com/IncepTools/go/core/di"
	"github.com/IncepTools/go/core/module"
	"github.com/IncepTools/go/examples/todo/apps/todo/tasks"
)

// App is the todo domain.
type App struct {
	module.BaseApp
}

// Descriptor is how a service mounts this domain: register every provider
// first, then build.
//
// The two phases are a correctness requirement rather than ceremony.
// Building a module constructs its controller, which resolves its service
// out of the container — so every provider in the whole application must be
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
		tasks.Providers(c),
	)
}

// NewApp wires the domain's modules. Every provider is registered by the
// time this runs.
func NewApp(s *di.Scope) *App {
	return &App{BaseApp: module.NewApp("todo", tasks.New(s))}
}

var _ module.App = (*App)(nil)
