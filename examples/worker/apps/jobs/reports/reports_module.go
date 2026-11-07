// Package reports is a module with NO HTTP surface.
//
// It declares no controllers and implements the lifecycle instead. A module
// does not have to serve routes to be a module: it still owns its
// dependencies, its schedule and its health.
package reports

import (
	"context"
	"time"

	"github.com/IncepTools/go/examples/worker/apps/jobs/reports/constants"
	"github.com/IncepTools/go/examples/worker/apps/jobs/reports/service"
	"github.com/IncepTools/go/src/core/di"
	"github.com/IncepTools/go/src/core/errs"
	"github.com/IncepTools/go/src/core/logx"
	"github.com/IncepTools/go/src/core/module"
	"github.com/IncepTools/go/src/platform/scheduler"
	"github.com/IncepTools/go/src/web/health"
)

// Module is the reports worker.
type Module struct {
	module.Base
	svc *service.ReportService
}

// Providers registers this module's dependencies.
func Providers(c *di.Container) error {
	return di.Provide(c, func(*di.Scope) (*service.ReportService, error) {
		return service.New(), nil
	})
}

// New builds the module and registers its jobs.
func New(s *di.Scope) *Module {
	svc := di.MustResolve[*service.ReportService](s)
	sched := di.MustResolve[*scheduler.Scheduler](s)

	// Every job is validated here, at boot. A malformed cron expression, a
	// duplicate name or a Singleton job with no locker fails the deploy
	// rather than the night it was meant to run.
	sched.MustAdd(scheduler.Job{
		Name:       constants.JobDaily,
		Every:      5 * time.Second, // a real one would be "0 2 * * *"
		RunOnStart: true,
		Singleton:  true,
		Timeout:    30 * time.Second,
		Jitter:     time.Second,
		Run:        svc.BuildDaily,
	})

	sched.MustAdd(scheduler.Job{
		Name:      constants.JobSweep,
		Cron:      "@every 20s",
		Singleton: true,
		Timeout:   time.Minute,
		Run:       svc.Sweep,
	})

	return &Module{
		svc: svc,
		Base: module.NewBase(constants.ModuleName,
			// No controllers. The module's whole surface is its schedule.
			module.WithHealthChecks(health.Of(constants.HealthCheck, health.Optional,
				func(context.Context) error {
					if _, failed := svc.Stats(); failed > constants.MaxFailedRuns {
						return errs.Unavailable("too many failed report runs")
					}
					return nil
				})),
		),
	}
}

// OnReady runs after the listener is up.
//
// Background work belongs here rather than in OnInit: a loop that starts
// before the process is serving races the boot it is part of.
func (m *Module) OnReady(ctx context.Context) error {
	logx.C(ctx).Info("reports worker ready")
	return nil
}

// OnShutdown runs on the way down, under the drain deadline.
func (m *Module) OnShutdown(ctx context.Context) error {
	built, failed := m.svc.Stats()
	logx.C(ctx).Info("reports worker stopping", "built", built, "failed", failed)
	return nil
}

var (
	_ module.Module = (*Module)(nil)
)
