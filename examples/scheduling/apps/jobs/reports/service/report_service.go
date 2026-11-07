// Package service holds the reports module's business rules.
//
// Note what it does NOT import: nothing from web/, and nothing from the
// scheduler. The schedule is a decision about WHEN to call this; the work
// itself takes a context and returns an error, so the same function serves a
// cron tick, a replay command and a test.
package service

import (
	"context"
	"sync"
	"time"

	"github.com/IncepTools/go/examples/scheduling/apps/jobs/reports/constants"
	"github.com/IncepTools/go/examples/scheduling/apps/jobs/reports/dto"
	"github.com/IncepTools/go/src/core/errs"
	"github.com/IncepTools/go/src/core/logx"
)

// ReportService builds reports and records what happened.
type ReportService struct {
	mu   sync.RWMutex
	runs map[string]*dto.RunStatus
}

// NewReportService builds the service.
func NewReportService() *ReportService {
	return &ReportService{runs: make(map[string]*dto.RunStatus)}
}

// BuildDaily is the work behind the daily job.
//
// It must be idempotent and it must respect cancellation. Both follow from
// the same fact: a scheduled job runs again whether or not the last one
// finished, and shutdown can arrive in the middle of one.
func (s *ReportService) BuildDaily(ctx context.Context) error {
	return s.track(ctx, constants.JobDaily, func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			// Returning the cancellation rather than finishing anyway is what
			// lets a deploy drain instead of hanging.
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
		logx.C(ctx).Info("daily report built")
		return nil
	})
}

// Cleanup is the work behind the cleanup job.
func (s *ReportService) Cleanup(ctx context.Context) error {
	return s.track(ctx, constants.JobCleanup, func(ctx context.Context) error {
		logx.C(ctx).Info("expired reports removed", "removed", 0)
		return nil
	})
}

// Status reports what each job last did.
func (s *ReportService) Status(_ context.Context, job string) (dto.RunStatus, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.runs[job]
	if !ok {
		return dto.RunStatus{}, errs.NotFound("no job named %q", job).WithCode("NO_SUCH_JOB")
	}
	return *st, nil
}

// All reports every job's last outcome.
func (s *ReportService) All(context.Context) []dto.RunStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]dto.RunStatus, 0, len(s.runs))
	for _, st := range s.runs {
		out = append(out, *st)
	}
	return out
}

// track runs fn and records the outcome, so a job that has been failing
// silently for a week is visible rather than merely absent from the logs.
func (s *ReportService) track(ctx context.Context, job string, fn func(context.Context) error) error {
	start := time.Now()
	err := fn(ctx)

	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.runs[job]
	if !ok {
		st = &dto.RunStatus{Job: job}
		s.runs[job] = st
	}
	now := time.Now()
	st.Runs++
	st.LastRun = &now
	st.Duration = time.Since(start).String()
	st.LastErr = ""
	if err != nil {
		st.LastErr = err.Error()
	}
	return err
}
