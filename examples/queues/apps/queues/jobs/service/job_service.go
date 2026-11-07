// Package service holds the jobs module's business rules.
//
// It is split in two on purpose, and the split follows the dependency
// direction. Processor does the work and needs no queue; JobService submits
// work and needs the queue. Keeping them together would be a cycle — the
// queue is constructed with the handler, so the handler cannot already hold
// the queue.
package service

import (
	"context"
	"encoding/json"

	"github.com/IncepTools/go/examples/queues/apps/queues/jobs/constants"
	"github.com/IncepTools/go/examples/queues/apps/queues/jobs/dto"
	"github.com/IncepTools/go/src/core/errs"
	"github.com/IncepTools/go/src/core/logx"
	"github.com/IncepTools/go/src/platform/queue"
)

// JobService submits work.
type JobService struct {
	q *queue.Queue
}

// NewJobService builds the submitting half.
func NewJobService(q *queue.Queue) *JobService { return &JobService{q: q} }

// Enqueue makes the job durable and returns immediately.
func (s *JobService) Enqueue(ctx context.Context, req dto.SubmitJob) (dto.JobReceipt, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return dto.JobReceipt{}, errs.Internal("encoding the job failed").WithCause(err)
	}
	id, err := s.q.Enqueue(ctx, body)
	if err != nil {
		return dto.JobReceipt{}, err
	}
	return dto.JobReceipt{ID: id, Kind: req.Kind}, nil
}

// Processor does the work.
type Processor struct{}

// NewProcessor builds the handling half.
func NewProcessor() *Processor { return &Processor{} }

// Process handles one delivery attempt.
//
// It must be idempotent. At-least-once delivery is the contract of every
// queue worth using, so this function will run twice for the same job sooner
// or later — a worker that crashes after doing the work but before
// acknowledging it is enough.
//
// Returning an error asks for a retry; returning nil acknowledges. A handler
// that swallows its errors reports success for work that did not happen, and
// nothing will ever retry it.
func (p *Processor) Process(ctx context.Context, msg queue.Message) error {
	var job dto.SubmitJob
	if err := json.Unmarshal(msg.Body, &job); err != nil {
		// Malformed payloads are not worth retrying — the bytes will not
		// improve — but the queue retries anything that errors, so this one
		// goes to the dead-letter handler after MaxAttempts.
		return errs.Invalid("job payload is not valid JSON").WithCause(err)
	}

	log := logx.C(ctx).With("queue", constants.QueueName, "jobId", msg.ID, "attempt", msg.Attempts)
	if job.Fail {
		log.Warn("job failed, will retry", "kind", job.Kind)
		return errs.Unavailable("downstream for %q is unavailable", job.Kind)
	}

	// Respect cancellation: the queue drains on shutdown, and a handler that
	// ignores its context is what makes a deploy hang until it is killed.
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	log.Info("job processed", "kind", job.Kind, "subject", job.Subject)
	return nil
}

// DeadLetter is the last stop for a job that exhausted its attempts.
//
// Dropping it silently here is the failure nobody notices until a customer
// asks where their export went. A real service persists the payload so it can
// be inspected and replayed.
func (p *Processor) DeadLetter(ctx context.Context, msg queue.Message, err error) {
	logx.C(ctx).Error("job dead-lettered",
		"queue", constants.QueueName, "jobId", msg.ID, "attempts", msg.Attempts,
		"body", string(msg.Body), "error", err)
}
