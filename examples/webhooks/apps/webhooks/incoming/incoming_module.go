// Package incoming is the inbound webhook module.
package incoming

import (
	"context"
	"os"

	"github.com/IncepTools/go/examples/webhooks/apps/webhooks/incoming/constants"
	"github.com/IncepTools/go/examples/webhooks/apps/webhooks/incoming/controller"
	"github.com/IncepTools/go/examples/webhooks/apps/webhooks/incoming/service"
	"github.com/IncepTools/go/src/core/di"
	"github.com/IncepTools/go/src/core/logx"
	"github.com/IncepTools/go/src/core/module"
	"github.com/IncepTools/go/src/platform/events"
)

// secret is the shared secret the sender signs with.
//
// It is read here rather than passed in from main, because it is this
// module's configuration and nothing else in the service has a use for it.
// A real deployment would fail to start without it instead of falling back;
// the default keeps the example runnable with nothing to set up.
func secret() string {
	if v := os.Getenv("WEBHOOK_SECRET"); v != "" {
		return v
	}
	return "development-secret"
}

// Module is the inbound webhook resource.
type Module struct {
	module.Base
	svc *service.WebhookService
}

// Providers registers this module's dependencies.
//
// It is a package function, not a method, because it must run BEFORE any
// module is constructed — New below resolves the service out of the
// container, so the service has to be registered first.
func Providers(c *di.Container) error {
	return di.Provide(c, func(s *di.Scope) (*service.WebhookService, error) {
		return service.NewWebhookService(secret(), di.MustResolve[*events.Bus](s)), nil
	})
}

// New builds the module.
func New(s *di.Scope) *Module {
	svc := di.MustResolve[*service.WebhookService](s)
	bus := di.MustResolve[*events.Bus](s)

	// Async, so a slow or failing subscriber cannot delay the 202 or make the
	// sender retry a delivery that was received perfectly well.
	events.On(bus, "webhooks.audit", events.Async,
		func(ctx context.Context, e service.DeliveryReceived) error {
			logx.C(ctx).Info("delivery received", "deliveryId", e.DeliveryID, "action", e.Action)
			return nil
		})

	return &Module{
		svc: svc,
		Base: module.NewBase(constants.ModuleName,
			module.WithControllers(controller.New(svc)),
		),
	}
}

var _ module.Module = (*Module)(nil)
