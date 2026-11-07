// Package jobs is the jobs domain.
package jobs

import (
	"context"
	"errors"

	"github.com/IncepTools/go/examples/scheduling/apps/jobs/reports"
	"github.com/IncepTools/go/src/core/di"
	"github.com/IncepTools/go/src/core/module"
	"github.com/IncepTools/go/src/platform/lock"
	"github.com/IncepTools/go/src/platform/scheduler"
)

// App is the jobs domain.
type App struct {
	module.BaseApp
	sched *scheduler.Scheduler
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
// The scheduler engine is provided here rather than in a module because every
// module in the domain adds jobs to the same one. Two engines would mean two
// sets of singleton locks, and a job declared on one would be invisible to
// the other.
//
// The in-memory locker is the single-instance implementation; a real
// deployment swaps in the Redis-backed one and no module changes.
func Providers(c *di.Container) error {
	return errors.Join(
		di.Provide(c, func(*di.Scope) (*scheduler.Scheduler, error) {
			return scheduler.New(lock.NewMemory()), nil
		}),
		reports.Providers(c),
	)
}

// NewApp wires the domain's modules. Building them is what declares their
// jobs, so the engine is fully populated by the time it starts.
func NewApp(s *di.Scope) *App {
	return &App{
		BaseApp: module.NewApp("jobs", reports.New(s)),
		sched:   di.MustResolve[*scheduler.Scheduler](s),
	}
}

// OnReady starts the scheduler after the listener is up.
//
// A mounted app is a lifecycle participant, so these two methods are the
// whole registration — main does not repeat it.
func (a *App) OnReady(ctx context.Context) error { return a.sched.OnReady(ctx) }

// OnShutdown stops scheduling and waits for the run in flight, so a deploy
// does not cut a job in half.
func (a *App) OnShutdown(ctx context.Context) error { return a.sched.OnShutdown(ctx) }

var _ module.App = (*App)(nil)
