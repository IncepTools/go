// Package reports is the scheduled reporting module.
//
// The schedule lives here, next to the work it runs. A job declared in main
// is a job nobody finds when they open the module that owns it — and when
// the module moves to another service, the schedule is left behind.
package reports

import (
	"time"

	"github.com/IncepTools/go/examples/scheduling/apps/jobs/reports/constants"
	"github.com/IncepTools/go/examples/scheduling/apps/jobs/reports/controller"
	"github.com/IncepTools/go/examples/scheduling/apps/jobs/reports/service"
	"github.com/IncepTools/go/src/core/di"
	"github.com/IncepTools/go/src/core/module"
	"github.com/IncepTools/go/src/platform/scheduler"
)

// Module is the scheduled reporting resource.
type Module struct {
	module.Base
	svc *service.ReportService
}

// Providers registers this module's dependencies.
//
// It is a package function, not a method, because it must run BEFORE any
// module is constructed — New below resolves the service out of the
// container, so the service has to be registered first.
func Providers(c *di.Container) error {
	return di.Provide(c, func(*di.Scope) (*service.ReportService, error) {
		return service.NewReportService(), nil
	})
}

// New builds the module and declares its schedule.
func New(s *di.Scope) *Module {
	svc := di.MustResolve[*service.ReportService](s)
	sched := di.MustResolve[*scheduler.Scheduler](s)

	// Singleton, so exactly one replica runs it. Without the lock every
	// replica builds the same report at the same instant — which is wasted
	// work at best, and duplicate side effects when the job sends anything.
	sched.MustAdd(scheduler.Job{
		Name:       constants.JobDaily,
		Every:      5 * time.Second,
		RunOnStart: true,
		Singleton:  true,
		// Jitter spreads replicas that all woke on the same tick, so they do
		// not contend for the lock at exactly the same moment.
		Jitter: 500 * time.Millisecond,
		// Timeout bounds a run. A job with no timeout and a hung dependency
		// holds its singleton lock forever, and the job never runs again.
		Timeout: 30 * time.Second,
		Run:     svc.BuildDaily,
	})

	// Not a singleton: removing expired rows is idempotent and local, so
	// there is nothing to protect against two replicas doing it at once.
	sched.MustAdd(scheduler.Job{
		Name:    constants.JobCleanup,
		Cron:    "0 4 * * *",
		Timeout: 5 * time.Minute,
		Run:     svc.Cleanup,
	})

	return &Module{
		svc: svc,
		Base: module.NewBase(constants.ModuleName,
			module.WithControllers(controller.New(svc)),
		),
	}
}

var _ module.Module = (*Module)(nil)
