// Package blog is the blog domain.
package blog

import (
	"errors"

	"github.com/IncepTools/go/examples/blog/apps/blog/posts"
	"github.com/IncepTools/go/src/core/di"
	"github.com/IncepTools/go/src/core/module"
)

// App is the blog domain.
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
		posts.Providers(c),
	)
}

// NewApp wires the domain's modules.
func NewApp(s *di.Scope) *App {
	return &App{BaseApp: module.NewApp("blog", posts.New(s))}
}

var _ module.App = (*App)(nil)
