// Package notifications is the order notification module.
//
// The subscriptions live here, in the module that owns both the event and its
// consequences. Registering them in main scatters the answer to "what happens
// when an order is placed" across two files, and the module stops being
// movable on its own.
package notifications

import (
	"github.com/IncepTools/go/examples/event-emitter/apps/events/notifications/constants"
	"github.com/IncepTools/go/examples/event-emitter/apps/events/notifications/controller"
	"github.com/IncepTools/go/examples/event-emitter/apps/events/notifications/service"
	"github.com/IncepTools/go/src/core/di"
	"github.com/IncepTools/go/src/core/module"
	"github.com/IncepTools/go/src/platform/events"
)

// Module is the order notification resource.
type Module struct {
	module.Base
	svc *service.OrderService
}

// Providers registers this module's dependencies.
//
// It is a package function, not a method, because it must run BEFORE any
// module is constructed — New below resolves the services out of the
// container, so they have to be registered first.
func Providers(c *di.Container) error {
	if err := di.Provide(c, func(*di.Scope) (*service.SubscriberService, error) {
		return service.NewSubscriberService(), nil
	}); err != nil {
		return err
	}
	return di.Provide(c, func(s *di.Scope) (*service.OrderService, error) {
		return service.NewOrderService(di.MustResolve[*events.Bus](s)), nil
	})
}

// New builds the module and registers its subscribers.
//
// Both are Async, and that is the normal case. A subscriber is a consequence
// of something that already happened, so it should not be able to slow the
// response or fail it. Sync exists for the handler whose failure genuinely
// belongs in the caller's error — but note that it still cannot prevent the
// async handlers from running, so a prerequisite belongs in the service
// instead. OrderService.Place has the detail.
func New(s *di.Scope) *Module {
	svc := di.MustResolve[*service.OrderService](s)
	subs := di.MustResolve[*service.SubscriberService](s)
	bus := di.MustResolve[*events.Bus](s)

	events.On(bus, constants.SubNotify, events.Async, subs.NotifyCustomer)
	events.On(bus, constants.SubAudit, events.Async, subs.Audit)

	return &Module{
		svc: svc,
		Base: module.NewBase(constants.ModuleName,
			module.WithControllers(controller.New(svc)),
		),
	}
}

var _ module.Module = (*Module)(nil)
