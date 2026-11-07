package scheduler_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/IncepTools/go/src/core/logx"
	"github.com/IncepTools/go/src/platform/lock"
	"github.com/IncepTools/go/src/platform/scheduler"
)

func TestInvalidJobsAreRefusedAtRegistration(t *testing.T) {
	s := scheduler.New(lock.NewMemory())
	noop := func(context.Context) error { return nil }

	cases := []struct {
		name string
		job  scheduler.Job
	}{
		{"no name", scheduler.Job{Every: time.Second, Run: noop}},
		{"no run", scheduler.Job{Name: "a", Every: time.Second}},
		{"neither schedule", scheduler.Job{Name: "a", Run: noop}},
		{"both schedules", scheduler.Job{Name: "a", Cron: "* * * * *", Every: time.Second, Run: noop}},
		{"bad cron", scheduler.Job{Name: "a", Cron: "not a cron", Run: noop}},
	}
	for _, c := range cases {
		if err := s.Add(c.job); err == nil {
			t.Errorf("%s: accepted", c.name)
		}
	}
}

func TestSingletonWithoutALockerIsRefused(t *testing.T) {
	s := scheduler.New(nil)
	err := s.Add(scheduler.Job{
		Name: "sweep", Every: time.Second, Singleton: true,
		Run: func(context.Context) error { return nil },
	})
	if err == nil {
		t.Fatal("a Singleton job without a locker was accepted; it would run on every replica")
	}
}

func TestDuplicateNameIsRefused(t *testing.T) {
	s := scheduler.New(lock.NewMemory())
	j := scheduler.Job{Name: "a", Every: time.Hour, Run: func(context.Context) error { return nil }}
	if err := s.Add(j); err != nil {
		t.Fatal(err)
	}
	if err := s.Add(j); err == nil {
		t.Fatal("two jobs with one name were accepted; they share a lock key")
	}
}

func TestRunOnStartFires(t *testing.T) {
	s := scheduler.New(lock.NewMemory())
	done := make(chan struct{})
	var once sync.Once

	if err := s.Add(scheduler.Job{
		Name: "boot", Every: time.Hour, RunOnStart: true,
		Run: func(context.Context) error { once.Do(func() { close(done) }); return nil },
	}); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer s.Stop(ctx) //nolint:errcheck

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunOnStart never fired")
	}
}

func TestASlowRunIsNotOverlapped(t *testing.T) {
	s := scheduler.New(lock.NewMemory())
	var concurrent, maxConcurrent atomic.Int32
	release := make(chan struct{})

	if err := s.Add(scheduler.Job{
		Name: "slow", Every: 10 * time.Millisecond, RunOnStart: true,
		Run: func(context.Context) error {
			n := concurrent.Add(1)
			for {
				m := maxConcurrent.Load()
				if n <= m || maxConcurrent.CompareAndSwap(m, n) {
					break
				}
			}
			<-release
			concurrent.Add(-1)
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(120 * time.Millisecond)
	close(release)

	stopCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_ = s.Stop(stopCtx)

	if got := maxConcurrent.Load(); got > 1 {
		t.Fatalf("max concurrent runs = %d; a slow job must skip, not pile up", got)
	}
}

func TestAPanickingJobDoesNotKillTheScheduler(t *testing.T) {
	s := scheduler.New(lock.NewMemory())
	runs := make(chan struct{}, 10)

	if err := s.Add(scheduler.Job{
		Name: "boom", Every: 10 * time.Millisecond, RunOnStart: true,
		Run: func(context.Context) error {
			select {
			case runs <- struct{}{}:
			default:
			}
			panic("job is broken")
		},
	}); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer s.Stop(ctx) //nolint:errcheck

	// It should fire more than once: the first panic must not stop the loop.
	deadline := time.After(2 * time.Second)
	count := 0
	for count < 2 {
		select {
		case <-runs:
			count++
		case <-deadline:
			t.Fatalf("the job fired %d time(s); a panic stopped the schedule", count)
		}
	}
}

func TestJobErrorsDoNotStopTheSchedule(t *testing.T) {
	s := scheduler.New(lock.NewMemory())
	var runs atomic.Int32

	if err := s.Add(scheduler.Job{
		Name: "flaky", Every: 10 * time.Millisecond, RunOnStart: true,
		Run: func(context.Context) error { runs.Add(1); return errors.New("nope") },
	}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	_ = s.Stop(ctx)

	if runs.Load() < 2 {
		t.Fatalf("runs = %d; a failing job must keep its schedule", runs.Load())
	}
}

func TestAddAfterStartIsRefused(t *testing.T) {
	s := scheduler.New(lock.NewMemory())
	ctx := context.Background()
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer s.Stop(ctx) //nolint:errcheck

	err := s.Add(scheduler.Job{Name: "late", Every: time.Hour, Run: func(context.Context) error { return nil }})
	if err == nil {
		t.Fatal("a job added after start was accepted but would never run")
	}
}

func TestShutdownCancellationIsNotReportedAsAFailure(t *testing.T) {
	// A routine deploy cancels whatever is running. Logging that as a job
	// failure pages somebody every time, which is how an alert stops being
	// read — so the scheduler distinguishes it.
	var buf bytes.Buffer
	logger := logx.New(logx.Options{Writer: &buf, Format: logx.FormatJSON, Level: slog.LevelDebug})
	ctx := logx.Into(context.Background(), logger)

	s := scheduler.New(lock.NewMemory())
	started := make(chan struct{})
	var once sync.Once

	if err := s.Add(scheduler.Job{
		Name: "slow", Every: time.Hour, RunOnStart: true, Timeout: time.Minute,
		Run: func(ctx context.Context) error {
			once.Do(func() { close(started) })
			<-ctx.Done()
			return ctx.Err()
		},
	}); err != nil {
		t.Fatal(err)
	}

	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	<-started

	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.Stop(stopCtx)

	out := buf.String()
	if strings.Contains(out, "scheduled job failed") {
		t.Fatalf("a job cancelled by shutdown was logged as a failure:\n%s", out)
	}
	if !strings.Contains(out, "stopped for shutdown") {
		t.Fatalf("the shutdown cancellation was not reported at all:\n%s", out)
	}
}
