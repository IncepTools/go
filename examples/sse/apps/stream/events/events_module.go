// Package events is the streaming module.
package events

import (
	"github.com/IncepTools/go/examples/sse/apps/stream/events/constants"
	"github.com/IncepTools/go/examples/sse/apps/stream/events/controller"
	"github.com/IncepTools/go/examples/sse/apps/stream/events/service"
	"github.com/IncepTools/go/src/core/di"
	"github.com/IncepTools/go/src/core/module"
)

// Module is the streaming resource.
type Module struct {
	module.Base
	svc *service.FeedService
}

// Providers registers this module's dependencies.
//
// It is a package function, not a method, because it must run BEFORE any
// module is constructed — New below resolves the service out of the
// container, so the service has to be registered first.
func Providers(c *di.Container) error {
	return di.Provide(c, func(*di.Scope) (*service.FeedService, error) {
		return service.NewFeedService(), nil
	})
}

// New builds the module.
func New(s *di.Scope) *Module {
	svc := di.MustResolve[*service.FeedService](s)
	return &Module{
		svc: svc,
		Base: module.NewBase(constants.ModuleName,
			module.WithControllers(controller.New(svc)),
		),
	}
}

var _ module.Module = (*Module)(nil)
