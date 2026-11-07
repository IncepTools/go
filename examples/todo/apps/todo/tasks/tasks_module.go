// Package tasks is the tasks resource module.
package tasks

import (
	"context"
	"time"

	"github.com/IncepTools/go/examples/todo/apps/todo/tasks/constants"
	"github.com/IncepTools/go/examples/todo/apps/todo/tasks/controller"
	"github.com/IncepTools/go/examples/todo/apps/todo/tasks/service"
	"github.com/IncepTools/go/src/core/di"
	"github.com/IncepTools/go/src/core/logx"
	"github.com/IncepTools/go/src/core/module"
	"github.com/IncepTools/go/src/platform/events"
	"github.com/IncepTools/go/src/platform/scheduler"
	"github.com/IncepTools/go/src/web/guard"
	"github.com/IncepTools/go/src/web/health"
)

// Module is the tasks resource.
type Module struct {
	module.Base
	svc *service.TaskService
}

// Providers registers this module's dependencies.
//
// It is a package function, not a method, because it must run BEFORE any
// module is constructed — New below resolves the service out of the
// container, so the service has to be registered first.
func Providers(c *di.Container) error {
	return di.Provide(c, func(s *di.Scope) (*service.TaskService, error) {
		return service.NewTaskService(di.MustResolve[*events.Bus](s)), nil
	})
}

// New builds the module.
func New(s *di.Scope) *Module {
	svc := di.MustResolve[*service.TaskService](s)
	bus := di.MustResolve[*events.Bus](s)
	sched := di.MustResolve[*scheduler.Scheduler](s)

	// Async, so completing a task is not slowed down by the notification —
	// and so a failing notifier cannot fail the request.
	events.On(bus, "tasks.notify-on-complete", events.Async,
		func(ctx context.Context, e service.TaskCompleted) error {
			logx.C(ctx).Info("task completed", "taskId", e.TaskID.String(), "title", e.Title)
			return nil
		})

	// Singleton, so exactly one replica purges — otherwise every replica
	// does the same work at the same moment.
	sched.MustAdd(scheduler.Job{
		Name:      "tasks.purge-completed",
		Cron:      "0 3 * * *",
		Singleton: true,
		Jitter:    30 * time.Second,
		Timeout:   5 * time.Minute,
		Run: func(ctx context.Context) error {
			_, err := svc.PurgeCompleted(ctx, time.Now().AddDate(0, 0, -30))
			return err
		},
	})

	return &Module{
		svc: svc,
		Base: module.NewBase(constants.ModuleName,
			module.WithControllers(controller.New(svc)),
			// Applied to every route in the module. Per-route guards in the
			// controller add to these rather than replacing them.
			module.WithGuards(guard.Authenticated),
			module.WithHealthChecks(health.Of("task-store", health.Critical,
				func(context.Context) error { return nil })),
		),
	}
}

var _ module.Module = (*Module)(nil)
