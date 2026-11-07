// Package service holds the reporting work.
//
// It is an ordinary struct with ordinary methods taking a context. Nothing
// here knows it is called from a schedule rather than a request — which is
// the point, and what makes it testable without a scheduler.
package service

import (
	"context"
	"math/rand/v2"
	"sync/atomic"
	"time"

	"github.com/IncepTools/go/src/core/errs"
	"github.com/IncepTools/go/src/core/logx"
)

// ReportService builds periodic reports.
type ReportService struct {
	built  atomic.Int64
	failed atomic.Int64
}

// New builds the service.
func New() *ReportService { return &ReportService{} }

// BuildDaily is the scheduled work.
//
// It honours the context between units. A job that ignores its deadline
// cannot be stopped: shutdown waits for it until the drain timeout and then
// abandons it mid-write.
func (s *ReportService) BuildDaily(ctx context.Context) error {
	log := logx.C(ctx)
	log.Info("building the daily report")

	for i := range 5 {
		if err := ctx.Err(); err != nil {
			log.Warn("stopping early", "completed", i, "reason", err.Error())
			return err
		}
		select {
		case <-time.After(150 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// A simulated upstream failure, to show that an error does not stop the
	// schedule: it is logged and the next occurrence still fires.
	if rand.IntN(4) == 0 { //nolint:gosec // illustrative, not security
		s.failed.Add(1)
		return errs.Unavailable("the metrics upstream did not answer").
			WithCode("METRICS_UNAVAILABLE")
	}

	s.built.Add(1)
	log.Info("daily report built", "total", s.built.Load())
	return nil
}

// Sweep removes expired rows. A second job, so the example shows two
// schedules with different shapes.
func (s *ReportService) Sweep(ctx context.Context) error {
	logx.C(ctx).Debug("sweeping expired rows")
	return nil
}

// Stats is what the health check reports.
func (s *ReportService) Stats() (built, failed int64) {
	return s.built.Load(), s.failed.Load()
}
