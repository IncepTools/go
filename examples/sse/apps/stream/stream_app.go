// Package stream is the stream domain.
package stream

import (
	"errors"

	"github.com/IncepTools/go/examples/sse/apps/stream/events"
	"github.com/IncepTools/go/src/core/di"
	"github.com/IncepTools/go/src/core/module"
)

// App is the stream domain.
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
		events.Providers(c),
	)
}

// NewApp wires the domain's modules.
func NewApp(s *di.Scope) *App {
	return &App{BaseApp: module.NewApp("stream", events.New(s))}
}

var _ module.App = (*App)(nil)
