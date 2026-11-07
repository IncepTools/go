// Package queues is the queues domain.
package queues

import (
	"errors"

	"github.com/IncepTools/go/examples/queues/apps/queues/jobs"
	"github.com/IncepTools/go/src/core/di"
	"github.com/IncepTools/go/src/core/module"
)

// App is the queues domain.
type App struct {
	module.BaseApp
}

// Descriptor is how a service mounts this domain: register every provider
// first, then build.
func Descriptor() module.Descriptor {
	return module.Descriptor{
		Providers: Providers,
		Build:     func(s *di.Scope) module.App { return NewApp(s) },
	}
}

// Providers registers everything this domain's modules resolve.
func Providers(c *di.Container) error {
	return errors.Join(
		jobs.Providers(c),
	)
}

// NewApp wires the domain's modules.
func NewApp(s *di.Scope) *App {
	return &App{BaseApp: module.NewApp("queues", jobs.New(s))}
}

var _ module.App = (*App)(nil)
