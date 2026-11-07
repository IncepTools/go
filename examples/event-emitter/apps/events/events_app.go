// Package events is the events domain.
package events

import (
	"errors"

	"github.com/IncepTools/go/examples/event-emitter/apps/events/notifications"
	"github.com/IncepTools/go/src/core/di"
	"github.com/IncepTools/go/src/core/module"
	eventbus "github.com/IncepTools/go/src/platform/events"
)

// App is the events domain.
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
//
// The bus is provided here rather than in a module because every module in
// the domain publishes and subscribes on the same one. Two buses would mean a
// publisher and its subscriber never meeting — and nothing would report an
// error, because a bus with no subscribers is a legitimate state.
func Providers(c *di.Container) error {
	return errors.Join(
		di.Provide(c, func(*di.Scope) (*eventbus.Bus, error) { return eventbus.New(), nil }),
		notifications.Providers(c),
	)
}

// NewApp wires the domain's modules. Building them is what registers their
// subscribers, so the bus is fully wired before the first request.
func NewApp(s *di.Scope) *App {
	return &App{BaseApp: module.NewApp("events", notifications.New(s))}
}

var _ module.App = (*App)(nil)
