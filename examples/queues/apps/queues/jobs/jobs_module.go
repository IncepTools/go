// Package jobs is the background job module.
//
// Everything the queue needs lives here: its options, its handler, its
// dead-letter policy and its draining. A module that owns a resource should
// own its whole life, so mounting the module is all a service has to do —
// there is no second step in main that is easy to forget and silently drops
// jobs when it is.
package jobs

import (
	"context"
	"time"

	"github.com/IncepTools/go/examples/queues/apps/queues/jobs/constants"
	"github.com/IncepTools/go/examples/queues/apps/queues/jobs/controller"
	"github.com/IncepTools/go/examples/queues/apps/queues/jobs/service"
	"github.com/IncepTools/go/src/core/di"
	"github.com/IncepTools/go/src/core/module"
	"github.com/IncepTools/go/src/platform/queue"
)

// Module is the background job resource.
type Module struct {
	module.Base
	svc *service.JobService
	q   *queue.Queue
}

// Providers registers this module's dependencies.
//
// It is a package function, not a method, because it must run BEFORE any
// module is constructed — New below resolves out of the container, so
// everything it resolves has to be registered first.
//
// The processor does not depend on the queue, so it can be built first and
// handed to the queue as its handler. The submitting half takes the queue.
// That is why the service is two types rather than one: a single type would
// have to hold the queue it is constructed with.
func Providers(c *di.Container) error {
	if err := di.Provide(c, func(*di.Scope) (*queue.Queue, error) {
		proc := service.NewProcessor()
		return queue.New(queue.Options{
			Name:        constants.QueueName,
			Workers:     2,
			MaxAttempts: 3,
			// Back off between attempts. Retrying instantly turns one failing
			// downstream into a tight loop against it.
			RetryDelay:   500 * time.Millisecond,
			OnDeadLetter: proc.DeadLetter,
		}, proc.Process)
	}); err != nil {
		return err
	}
	return di.Provide(c, func(s *di.Scope) (*service.JobService, error) {
		return service.NewJobService(di.MustResolve[*queue.Queue](s)), nil
	})
}

// New builds the module.
func New(s *di.Scope) *Module {
	svc := di.MustResolve[*service.JobService](s)
	return &Module{
		svc: svc,
		q:   di.MustResolve[*queue.Queue](s),
		Base: module.NewBase(constants.ModuleName,
			module.WithControllers(controller.New(svc)),
		),
	}
}

// OnReady starts the workers, after the listener is up.
//
// A mounted module is registered as a lifecycle participant, so implementing
// these two methods is the whole registration — main does not repeat it.
func (m *Module) OnReady(ctx context.Context) error { return m.q.OnReady(ctx) }

// OnShutdown drains in-flight jobs before the process exits. Without it a
// deploy drops whatever was mid-attempt.
func (m *Module) OnShutdown(ctx context.Context) error { return m.q.OnShutdown(ctx) }

var _ module.Module = (*Module)(nil)
