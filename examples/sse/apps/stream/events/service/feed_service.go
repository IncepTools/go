// Package service holds the events module's business rules.
//
// Note what it does NOT import: nothing from web/, and nothing from the SSE
// package. The feed emits values through a callback, so the same producer
// serves an SSE stream, a WebSocket, a test that collects into a slice, or a
// batch job that writes them to a file.
package service

import (
	"context"
	"time"

	"github.com/IncepTools/go/examples/sse/apps/stream/events/dto"
	"github.com/IncepTools/go/src/core/errs"
)

// MinInterval bounds how fast a client may ask to be fed.
//
// Without a floor, a client asking for 1ms makes the server generate a
// thousand events a second for one connection, and the cost is the server's.
const MinInterval = 250 * time.Millisecond

// MaxInterval bounds the other end, so a connection cannot sit silent long
// enough for a proxy to decide it is dead.
const MaxInterval = 30 * time.Second

// FeedService produces the tick stream.
type FeedService struct{}

// NewFeedService builds the service.
func NewFeedService() *FeedService { return &FeedService{} }

// Interval validates a requested interval and applies the default.
func (s *FeedService) Interval(requested time.Duration) (time.Duration, error) {
	if requested == 0 {
		return time.Second, nil
	}
	if requested < MinInterval || requested > MaxInterval {
		return 0, errs.Invalid("interval must be between %s and %s", MinInterval, MaxInterval).
			WithCode("BAD_INTERVAL")
	}
	return requested, nil
}

// Stream emits ticks until the context is cancelled or emit fails.
//
// Cancellation is the normal exit, not an error: a browser closing the tab
// cancels the request context, and a producer that treats that as a failure
// fills the logs with alarm about clients behaving correctly. Resuming from
// `after` is what makes Last-Event-ID work.
func (s *FeedService) Stream(ctx context.Context, interval time.Duration, after int, emit func(dto.Tick) error) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for seq := after + 1; ; seq++ {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			tick := dto.Tick{
				Sequence: seq,
				Symbol:   "ACME",
				Price:    10_000 + int64(seq%100)*7,
				At:       now.UTC(),
			}
			if err := emit(tick); err != nil {
				// The client went away mid-write. Stop producing rather than
				// looping against a dead connection.
				return err
			}
		}
	}
}
