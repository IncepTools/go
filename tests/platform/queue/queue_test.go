package queue_test

import (
	"context"
	"github.com/IncepTools/go/src/platform/queue"
	"sync/atomic"
	"testing"
	"time"
)

func TestQueueRetriesAndDeadLetters(t *testing.T) {
	var calls, dead atomic.Int32
	q, err := queue.New(queue.Options{Name: "test", Workers: 1, MaxAttempts: 2, RetryDelay: time.Millisecond, OnDeadLetter: func(context.Context, queue.Message, error) { dead.Add(1) }}, func(context.Context, queue.Message) error { calls.Add(1); return context.Canceled })
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Enqueue(context.Background(), []byte("job")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for dead.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if calls.Load() != 2 || dead.Load() != 1 {
		t.Fatalf("calls=%d dead=%d", calls.Load(), dead.Load())
	}
	if err := q.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}
