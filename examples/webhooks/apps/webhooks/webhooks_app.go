// Package webhooks is the webhooks domain.
package webhooks

import (
	"errors"

	"github.com/IncepTools/go/examples/webhooks/apps/webhooks/incoming"
	"github.com/IncepTools/go/src/core/di"
	"github.com/IncepTools/go/src/core/module"
	"github.com/IncepTools/go/src/platform/events"
)

// App is the webhooks domain.
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
//
// The event bus is provided here rather than in a module because it is shared
// by every module in the domain: two modules that each built their own would
// publish into separate buses and never see each other's events. Anything
// used by exactly one module belongs in that module's Providers instead.
func Providers(c *di.Container) error {
	return errors.Join(
		di.Provide(c, func(*di.Scope) (*events.Bus, error) { return events.New(), nil }),
		incoming.Providers(c),
	)
}

// NewApp wires the domain's modules. Every provider is registered by the time
// this runs.
func NewApp(s *di.Scope) *App {
	return &App{BaseApp: module.NewApp("webhooks", incoming.New(s))}
}

var _ module.App = (*App)(nil)
