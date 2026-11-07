// Package hello is the greeting module.
package hello

import (
	"github.com/IncepTools/go/examples/minimal/apps/greeter/hello/constants"
	"github.com/IncepTools/go/examples/minimal/apps/greeter/hello/controller"
	"github.com/IncepTools/go/examples/minimal/apps/greeter/hello/service"
	"github.com/IncepTools/go/src/core/di"
	"github.com/IncepTools/go/src/core/module"
)

// Module is the greeting resource.
type Module struct {
	module.Base
	svc *service.GreeterService
}

// Providers registers this module's dependencies.
//
// It is a package function, not a method, because it must run BEFORE any
// module is constructed — New below resolves the service out of the
// container, so the service has to be registered first.
func Providers(c *di.Container) error {
	return di.Provide(c, func(*di.Scope) (*service.GreeterService, error) {
		return service.NewGreeterService(), nil
	})
}

// New builds the module.
func New(s *di.Scope) *Module {
	svc := di.MustResolve[*service.GreeterService](s)
	return &Module{
		svc: svc,
		Base: module.NewBase(constants.ModuleName,
			module.WithControllers(controller.New(svc)),
		),
	}
}

var _ module.Module = (*Module)(nil)
