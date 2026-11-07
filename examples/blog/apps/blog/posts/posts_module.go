// Package posts is the posts resource module.
package posts

import (
	"gorm.io/gorm"

	"github.com/IncepTools/go/examples/blog/apps/blog/posts/constants"
	"github.com/IncepTools/go/examples/blog/apps/blog/posts/controller"
	"github.com/IncepTools/go/examples/blog/apps/blog/posts/entity"
	"github.com/IncepTools/go/examples/blog/apps/blog/posts/repository"
	"github.com/IncepTools/go/examples/blog/apps/blog/posts/service"
	"github.com/IncepTools/go/src/core/di"
	"github.com/IncepTools/go/src/core/module"
	"github.com/IncepTools/go/src/web/guard"
)

// Module is the posts resource.
type Module struct {
	module.Base
}

// Providers registers this module's dependencies.
//
// A package function, not a method: it must run before any module is
// constructed, because New below resolves the service out of the container.
func Providers(c *di.Container) error {
	if err := di.Provide(c, func(s *di.Scope) (*repository.PostRepository, error) {
		return repository.New(di.MustResolve[*gorm.DB](s)), nil
	}); err != nil {
		return err
	}
	return di.Provide(c, func(s *di.Scope) (*service.PostService, error) {
		return service.New(
			di.MustResolve[*repository.PostRepository](s),
			di.MustResolve[*gorm.DB](s),
		), nil
	})
}

// New builds the module.
func New(s *di.Scope) *Module {
	ctrl := controller.New(di.MustResolve[*service.PostService](s))
	return &Module{
		Base: module.NewBase(constants.ModuleName,
			module.WithControllers(ctrl),
			// Declares which table this module owns. Metadata, not
			// migration — the schema comes from migrations/.
			module.WithEntities(&entity.Post{}),
			module.WithGuards(guard.Authenticated),
		),
	}
}

var (
	_ module.Module      = (*Module)(nil)
	_ module.EntityOwner = (*Module)(nil)
)
