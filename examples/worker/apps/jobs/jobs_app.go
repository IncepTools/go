// Package jobs is the background-work domain.
package jobs

import (
	"errors"

	"github.com/IncepTools/go/examples/worker/apps/jobs/reports"
	"github.com/IncepTools/go/src/core/di"
	"github.com/IncepTools/go/src/core/module"
)

// App is the jobs domain.
type App struct {
	module.BaseApp
}

// Descriptor is how a service mounts this domain.
func Descriptor() module.Descriptor {
	return module.Descriptor{
		Providers: Providers,
		Build:     func(s *di.Scope) module.App { return NewApp(s) },
	}
}

// Providers registers everything this domain's modules resolve.
func Providers(c *di.Container) error {
	return errors.Join(
		reports.Providers(c),
	)
}

// NewApp wires the domain's modules.
func NewApp(s *di.Scope) *App {
	return &App{BaseApp: module.NewApp("jobs", reports.New(s))}
}

var _ module.App = (*App)(nil)
