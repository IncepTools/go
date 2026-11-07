// Package quotes is the quote module.
//
// It owns its own reloadable configuration. A mounted module is a lifecycle
// participant, so implementing OnReload is the entire registration: SIGHUP on
// a server and Application.Reload in a function both reach this method, and
// main does not mention it.
package quotes

import (
	"context"

	"github.com/IncepTools/go/examples/serverless/apps/catalog/quotes/constants"
	"github.com/IncepTools/go/examples/serverless/apps/catalog/quotes/controller"
	"github.com/IncepTools/go/examples/serverless/apps/catalog/quotes/service"
	"github.com/IncepTools/go/src/core/di"
	"github.com/IncepTools/go/src/core/module"
)

// Module is the quote resource.
type Module struct {
	module.Base
	svc *service.QuoteService
}

// Providers registers this module's dependencies.
//
// It is a package function, not a method, because it must run BEFORE any
// module is constructed — New below resolves the service out of the
// container, so the service has to be registered first.
//
// In a function runtime this runs once per cold start, not once per
// invocation, which is what makes a pooled client here worth having.
func Providers(c *di.Container) error {
	return di.Provide(c, func(*di.Scope) (*service.QuoteService, error) {
		return service.NewQuoteService(), nil
	})
}

// New builds the module.
func New(s *di.Scope) *Module {
	svc := di.MustResolve[*service.QuoteService](s)
	return &Module{
		svc: svc,
		Base: module.NewBase(constants.ModuleName,
			module.WithControllers(controller.New(svc)),
		),
	}
}

// OnReload refreshes this module's configuration in place.
//
// Failures are reported and the process keeps serving the last known-good
// configuration, so a bad parameter store does not take the service down with
// it. Other modules reload independently of this one.
func (m *Module) OnReload(ctx context.Context) error { return m.svc.Reload(ctx) }

var _ module.Module = (*Module)(nil)
