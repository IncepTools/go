// Package queue provides a small lifecycle-aware in-process work queue.
//
// It is deliberately an application primitive, not a pretend durable broker:
// messages in memory are lost when the process exits. Use the outbox package
// (or a real broker) when a job must survive a crash.
package queue

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Message is one queued payload. Attempts starts at one when a worker begins
// processing it.
type Message struct {
	ID       string
	Body     []byte
	Attempts int
}

// Handler processes a message. Returning an error retries it until
// MaxAttempts is reached.
type Handler func(context.Context, Message) error

// Options configures a Queue.
type Options struct {
	Name         string
	Workers      int
	Buffer       int
	MaxAttempts  int
	RetryDelay   time.Duration
	OnDeadLetter func(context.Context, Message, error)
}

// Queue is safe for concurrent producers and consumers.
type Queue struct {
	name string
	h    Handler
	opt  Options

	mu      sync.Mutex
	started bool
	closed  bool
	in      chan Message
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// New constructs a queue. It starts when registered with a lifecycle registry
// and OnReady runs, or when Start is called directly.
func New(opt Options, h Handler) (*Queue, error) {
	if opt.Name == "" {
		return nil, errors.New("queue: name is required")
	}
	if h == nil {
		return nil, errors.New("queue: handler is required")
	}
	if opt.Workers <= 0 {
		opt.Workers = 1
	}
	if opt.Buffer <= 0 {
		opt.Buffer = 128
	}
	if opt.MaxAttempts <= 0 {
		opt.MaxAttempts = 3
	}
	if opt.RetryDelay <= 0 {
		opt.RetryDelay = time.Second
	}
	return &Queue{name: opt.Name, h: h, opt: opt, in: make(chan Message, opt.Buffer)}, nil
}

// Name identifies the queue in logs and diagnostics.
func (q *Queue) Name() string { return q.name }

// Enqueue adds a payload, returning when it has entered the queue buffer.
// Enqueue is valid before Start, which makes boot-time producers convenient.
func (q *Queue) Enqueue(ctx context.Context, body []byte) (string, error) {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return "", errors.New("queue: closed")
	}
	id := uuid.NewString()
	msg := Message{ID: id, Body: append([]byte(nil), body...)}
	in := q.in
	// Keep the state lock while sending. Stop also takes it before closing the
	// input channel, so a producer can never race a close and panic.
	select {
	case in <- msg:
		q.mu.Unlock()
		return id, nil
	case <-ctx.Done():
		q.mu.Unlock()
		return "", ctx.Err()
	}
}

// OnReady implements lifecycle.Ready.
func (q *Queue) OnReady(ctx context.Context) error { return q.Start(ctx) }

// Start launches workers. The parent is detached from request cancellation.
func (q *Queue) Start(parent context.Context) error {
	q.mu.Lock()
	if q.started {
		q.mu.Unlock()
		return errors.New("queue: already started")
	}
	if q.closed {
		q.mu.Unlock()
		return errors.New("queue: closed")
	}
	q.started = true
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	q.cancel = cancel
	workers := q.opt.Workers
	q.mu.Unlock()
	for i := 0; i < workers; i++ {
		q.wg.Add(1)
		go q.worker(ctx)
	}
	return nil
}

// OnShutdown stops accepting work and drains messages already buffered.
func (q *Queue) OnShutdown(ctx context.Context) error { return q.Stop(ctx) }

// Stop cancels workers after the input buffer is drained. A handler should
// honor its context so shutdown remains bounded by the application deadline.
func (q *Queue) Stop(ctx context.Context) error {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return nil
	}
	q.closed = true
	cancel := q.cancel
	started := q.started
	close(q.in)
	q.mu.Unlock()
	if !started || cancel == nil {
		return nil
	}
	// Closing input drains buffered messages. Cancellation is reserved for the
	// deadline path below, so successful shutdown does not abandon queued work.
	done := make(chan struct{})
	go func() { q.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		cancel()
		return fmt.Errorf("queue: workers did not stop: %w", ctx.Err())
	}
}

func (q *Queue) worker(ctx context.Context) {
	defer q.wg.Done()
	for msg := range q.in {
		q.process(ctx, msg)
	}
}

func (q *Queue) process(ctx context.Context, msg Message) {
	for msg.Attempts < q.opt.MaxAttempts {
		msg.Attempts++
		if err := q.h(ctx, msg); err == nil {
			return
		} else if msg.Attempts == q.opt.MaxAttempts {
			if q.opt.OnDeadLetter != nil {
				q.opt.OnDeadLetter(ctx, msg, err)
			}
			return
		}
		t := time.NewTimer(q.opt.RetryDelay)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}
