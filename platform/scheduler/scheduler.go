// Package scheduler runs recurring work on a cron expression or a fixed
// interval, with the two properties that decide whether scheduled work is
// safe to deploy on more than one replica.
//
//	Singleton   one replica runs the job, not all of them.
//	Overlap     a slow run does not start a second copy of itself.
//
// Both failure modes are invisible in development, where there is one replica
// and every job finishes in a millisecond. In production they are how a
// nightly billing sweep charges three times, and how a reconciler that
// normally takes 20 seconds piles up 200 copies the night it takes 20 minutes.
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"runtime/debug"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/IncepTools/go/core/ctxkit"
	"github.com/IncepTools/go/core/logx"
	"github.com/IncepTools/go/platform/lock"
)

// Job is one recurring task.
type Job struct {
	// Name identifies the job in logs and is the lock key. Required, unique.
	Name string
	// Cron is a standard 5-field expression ("0 3 * * *"). Set this or Every.
	//
	// Descriptors are supported: @hourly, @daily, @weekly, @every 1h30m.
	Cron string
	// Every runs at a fixed interval from start-up. Set this or Cron.
	Every time.Duration
	// Run does the work. It MUST honour ctx — a job that ignores its deadline
	// cannot be stopped, and shutdown will wait for it until the drain
	// timeout and then abandon it mid-write.
	Run func(ctx context.Context) error

	// Singleton makes exactly one replica run each occurrence, via the
	// scheduler's Locker. Leave it off ONLY for work that is safe to run N
	// times — a cache warm, a metric refresh.
	Singleton bool
	// LockTTL must exceed the job's worst-case runtime. Too short and the
	// lock expires mid-run, letting a second replica start: the exact thing
	// Singleton was for. Defaults to Timeout, or 5 minutes.
	LockTTL time.Duration
	// Timeout bounds one run. Defaults to 5 minutes; zero is not unlimited,
	// because an unbounded scheduled job is how a connection pool is
	// exhausted overnight.
	Timeout time.Duration
	// RunOnStart fires once at start-up, in addition to the schedule.
	RunOnStart bool
	// Jitter spreads the start across replicas by up to this much. With
	// Singleton it avoids a thundering herd on the lock; without it, it
	// avoids every replica hitting the database on the same second.
	Jitter time.Duration
}

// Scheduler owns the running jobs. Register it as a lifecycle hook: it starts
// in OnReady and drains in OnShutdown.
type Scheduler struct {
	locker lock.Locker
	parser cron.Parser

	mu      sync.Mutex
	jobs    []Job
	started bool
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	// running guards against overlap, per job.
	running sync.Map
}

// New builds a scheduler. Pass a lock.Locker for Singleton jobs; without one,
// Singleton jobs are refused at Add rather than silently running everywhere.
func New(locker lock.Locker) *Scheduler {
	return &Scheduler{
		locker: locker,
		parser: cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor),
	}
}

// Add registers a job. It validates eagerly, so a malformed cron expression
// fails the boot instead of being discovered the night it was supposed to run.
func (s *Scheduler) Add(j Job) error {
	if j.Name == "" {
		return errors.New("scheduler: job has no name")
	}
	if j.Run == nil {
		return fmt.Errorf("scheduler: job %q has no Run function", j.Name)
	}
	if (j.Cron == "") == (j.Every == 0) {
		return fmt.Errorf("scheduler: job %q must set exactly one of Cron or Every", j.Name)
	}
	if j.Cron != "" {
		if _, err := s.parser.Parse(j.Cron); err != nil {
			return fmt.Errorf("scheduler: job %q has an invalid cron expression %q: %w", j.Name, j.Cron, err)
		}
	}
	if j.Singleton && s.locker == nil {
		return fmt.Errorf("scheduler: job %q is Singleton but the scheduler has no Locker", j.Name)
	}
	if j.Timeout <= 0 {
		j.Timeout = 5 * time.Minute
	}
	if j.LockTTL <= 0 {
		j.LockTTL = j.Timeout
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return fmt.Errorf("scheduler: job %q added after start", j.Name)
	}
	for _, existing := range s.jobs {
		if existing.Name == j.Name {
			return fmt.Errorf("scheduler: two jobs named %q", j.Name)
		}
	}
	s.jobs = append(s.jobs, j)
	return nil
}

// MustAdd is Add, panicking on an invalid job. Jobs are declared in code at
// boot, so a panic there is the right failure.
func (s *Scheduler) MustAdd(j Job) {
	if err := s.Add(j); err != nil {
		panic(err)
	}
}

// Jobs lists the registered job names.
func (s *Scheduler) Jobs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.jobs))
	for i, j := range s.jobs {
		out[i] = j.Name
	}
	return out
}

// OnReady implements lifecycle.Ready: it starts every job.
//
// Ready rather than Init on purpose — a reconciler that starts before the
// listener races the boot it is part of, and will happily run against
// half-constructed dependencies.
func (s *Scheduler) OnReady(ctx context.Context) error { return s.Start(ctx) }

// Start begins running jobs. The context it derives from must outlive the
// jobs; pass the application context, not a request's.
func (s *Scheduler) Start(parent context.Context) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return errors.New("scheduler: already started")
	}
	s.started = true
	jobs := append([]Job(nil), s.jobs...)
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	s.cancel = cancel
	s.mu.Unlock()

	for _, j := range jobs {
		s.wg.Add(1)
		go func(j Job) {
			defer s.wg.Done()
			s.loop(ctx, j)
		}(j)
	}
	logx.C(parent).Info("scheduler started", "jobs", len(jobs))
	return nil
}

func (s *Scheduler) loop(ctx context.Context, j Job) {
	if j.Jitter > 0 {
		select {
		case <-ctx.Done():
			return
		// math/rand is correct here: jitter spreads start times, it does not
		// protect anything. Draining the system entropy pool to decide when a
		// cron job begins would be a cost with no benefit.
		case <-time.After(time.Duration(rand.Int64N(int64(j.Jitter) + 1))): //nolint:gosec
		}
	}
	if j.RunOnStart {
		s.fire(ctx, j)
	}

	if j.Every > 0 {
		t := time.NewTicker(j.Every)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.fire(ctx, j)
			}
		}
	}

	sched, err := s.parser.Parse(j.Cron)
	if err != nil {
		// Add validated this; reaching here means the schedule changed under
		// us, which should be impossible. Log loudly rather than spin.
		logx.C(ctx).Error("scheduler: job has an unparsable schedule", "job", j.Name, "error", err.Error())
		return
	}
	for {
		next := sched.Next(time.Now())
		wait := time.Until(next)
		if wait < 0 {
			wait = time.Second
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			s.fire(ctx, j)
		}
	}
}

func (s *Scheduler) fire(ctx context.Context, j Job) {
	// Overlap guard. A run still going when the next is due is skipped, not
	// queued: queueing a slow job behind itself converts a slowdown into an
	// unbounded backlog.
	if _, busy := s.running.LoadOrStore(j.Name, true); busy {
		logx.C(ctx).Warn("scheduled job skipped: the previous run is still going",
			"job", j.Name)
		return
	}
	defer s.running.Delete(j.Name)

	runCtx, cancel := context.WithTimeout(ctx, j.Timeout)
	defer cancel()
	runCtx = ctxkit.WithTrace(runCtx, "job-"+j.Name+"-"+time.Now().UTC().Format("20060102T150405"))

	start := time.Now()
	var err error
	if j.Singleton {
		var ran bool
		ran, err = lock.Do(runCtx, s.locker, "job:"+j.Name, j.LockTTL, func(c context.Context) error {
			return s.safeRun(c, j)
		})
		if err == nil && !ran {
			logx.C(runCtx).Debug("scheduled job is running on another replica", "job", j.Name)
			return
		}
	} else {
		err = s.safeRun(runCtx, j)
	}

	elapsed := time.Since(start)
	switch {
	case err != nil:
		logx.Err(runCtx, "scheduled job failed", err, "job", j.Name, "ms", elapsed.Milliseconds())
	default:
		logx.C(runCtx).Info("scheduled job finished", "job", j.Name, "ms", elapsed.Milliseconds())
	}
}

func (s *Scheduler) safeRun(ctx context.Context, j Job) (err error) {
	defer func() {
		if r := recover(); r != nil {
			logx.C(ctx).Error("scheduled job panicked",
				"job", j.Name, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			err = fmt.Errorf("scheduler: job %s panicked", j.Name)
		}
	}()
	return j.Run(ctx)
}

// OnShutdown implements lifecycle.Shutdowner: stop scheduling and wait for
// the runs in flight, up to the drain deadline.
func (s *Scheduler) OnShutdown(ctx context.Context) error { return s.Stop(ctx) }

// Stop halts scheduling and waits for running jobs.
func (s *Scheduler) Stop(ctx context.Context) error {
	s.mu.Lock()
	cancel := s.cancel
	started := s.started
	s.mu.Unlock()
	if !started || cancel == nil {
		return nil
	}
	cancel()

	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return errors.New("scheduler: jobs still running at the drain deadline")
	}
}
