// Package outbox makes a side effect as durable as the write that caused it.
//
// The problem: a handler writes a row and then publishes an event. Between
// those two lines the process can die, and you have a resource that exists
// with nothing downstream knowing. Swap the order and you have an event for a
// resource that does not exist. There is no ordering of two systems that
// fixes this — the write and the publish must be ONE atomic act.
//
// The fix: write the message into a table in the SAME transaction as the
// state change. Either both land or neither does. A relay then reads the
// table and publishes, retrying until it succeeds.
//
// What this buys is at-least-once delivery, which means a consumer MUST be
// idempotent — the relay can publish a message twice if it crashes between
// publishing and marking. Exactly-once does not exist here; at-least-once
// plus an idempotent consumer is the design.
package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/IncepTools/go/src/core/ctxkit"
	"github.com/IncepTools/go/src/core/logx"
	"github.com/IncepTools/go/src/data/repo"
)

// Message is one pending side effect.
//
// Migration:
//
//	CREATE TABLE outbox_messages (
//	    id            UUID PRIMARY KEY,
//	    topic         TEXT NOT NULL,
//	    payload       JSONB NOT NULL,
//	    trace_id      TEXT,
//	    attempts      INTEGER NOT NULL DEFAULT 0,
//	    last_error    TEXT,
//	    available_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
//	    published_at  TIMESTAMPTZ,
//	    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
//	);
//	CREATE INDEX outbox_pending_idx ON outbox_messages (available_at)
//	    WHERE published_at IS NULL;
type Message struct {
	ID      uuid.UUID `gorm:"primaryKey;column:id"`
	Topic   string    `gorm:"column:topic"`
	Payload []byte    `gorm:"column:payload;type:jsonb"`
	// TraceID carries the originating request's id, so the eventual publish
	// can be correlated back to the API call that caused it — hours later,
	// in a different process.
	TraceID     string     `gorm:"column:trace_id"`
	Attempts    int        `gorm:"column:attempts"`
	LastError   string     `gorm:"column:last_error"`
	AvailableAt time.Time  `gorm:"column:available_at"`
	PublishedAt *time.Time `gorm:"column:published_at"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
}

// TableName implements gorm's Tabler.
func (Message) TableName() string { return "outbox_messages" }

// Enqueue writes a message on the transaction already on ctx.
//
// It MUST be called inside repo.Transaction, and refuses otherwise. Outside a
// transaction it is just a slower publish with the same hole in the middle,
// and silently allowing that would defeat the entire package.
func Enqueue(ctx context.Context, db *gorm.DB, topic string, payload any) error {
	if !repo.InTx(ctx) {
		return fmt.Errorf("outbox: Enqueue(%s) outside a transaction; "+
			"the message must be written atomically with the state change it describes", topic)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("outbox: encoding %s: %w", topic, err)
	}
	msg := Message{
		ID:          uuid.New(),
		Topic:       topic,
		Payload:     raw,
		TraceID:     ctxkit.Trace(ctx),
		AvailableAt: time.Now().UTC(),
		CreatedAt:   time.Now().UTC(),
	}
	return repo.TxFrom(ctx, db).WithContext(ctx).Create(&msg).Error
}

// Publisher delivers a message to wherever it is going: a broker, a webhook,
// an email provider, an in-process bus.
//
// An error means "try again later". Return ErrDrop for a message that will
// never succeed — a malformed payload from an old deployment — so the relay
// stops retrying it forever.
type Publisher interface {
	Publish(ctx context.Context, m Message) error
}

// PublisherFunc adapts a function to Publisher.
type PublisherFunc func(ctx context.Context, m Message) error

// Publish implements Publisher.
func (f PublisherFunc) Publish(ctx context.Context, m Message) error { return f(ctx, m) }

// ErrDrop tells the relay to stop retrying a message.
var ErrDrop = errors.New("outbox: drop this message")

// RelayOptions configures the relay.
type RelayOptions struct {
	DB        *gorm.DB
	Publisher Publisher
	// Batch is how many messages one pass claims. Default 50.
	Batch int
	// Interval between passes. Default 5s.
	Interval time.Duration
	// MaxAttempts before a message is parked (left unpublished, with its
	// error, for a human). Default 10. Parking rather than deleting is
	// deliberate: a message nobody can explain is evidence.
	MaxAttempts int
	// Backoff is the base delay between attempts; it doubles each time.
	// Default 10s.
	Backoff time.Duration
}

// Relay polls the table and publishes.
//
// It is safe to run on every replica: each pass claims rows with
// FOR UPDATE SKIP LOCKED, so two relays take disjoint batches rather than
// fighting or double-publishing. That is why this needs no distributed lock.
type Relay struct {
	opt    RelayOptions
	cancel context.CancelFunc
	done   chan struct{}
}

// NewRelay builds a relay.
func NewRelay(opt RelayOptions) *Relay {
	if opt.Batch <= 0 {
		opt.Batch = 50
	}
	if opt.Interval <= 0 {
		opt.Interval = 5 * time.Second
	}
	if opt.MaxAttempts <= 0 {
		opt.MaxAttempts = 10
	}
	if opt.Backoff <= 0 {
		opt.Backoff = 10 * time.Second
	}
	return &Relay{opt: opt, done: make(chan struct{})}
}

// OnReady implements lifecycle.Ready.
func (r *Relay) OnReady(ctx context.Context) error {
	loopCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	r.cancel = cancel
	go r.loop(loopCtx)
	return nil
}

// OnShutdown implements lifecycle.Shutdowner.
func (r *Relay) OnShutdown(ctx context.Context) error {
	if r.cancel == nil {
		return nil
	}
	r.cancel()
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return errors.New("outbox: relay still draining at the deadline")
	}
}

func (r *Relay) loop(ctx context.Context) {
	defer close(r.done)
	t := time.NewTicker(r.opt.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n, err := r.Pass(ctx); err != nil {
				logx.C(ctx).Error("outbox pass failed", "error", err.Error())
			} else if n > 0 {
				logx.C(ctx).Debug("outbox pass", "published", n)
			}
		}
	}
}

// Pass claims and publishes one batch, returning how many were published.
// Exported so a test can drive the relay deterministically instead of waiting
// on a ticker.
func (r *Relay) Pass(ctx context.Context) (int, error) {
	var claimed []Message

	// Claim inside a transaction so SKIP LOCKED means something: the lock is
	// held for the length of the transaction, and another replica's SELECT
	// walks past these rows instead of blocking on them.
	err := r.opt.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate, Options: "SKIP LOCKED"}).
			Where("published_at IS NULL AND available_at <= ? AND attempts < ?",
				time.Now().UTC(), r.opt.MaxAttempts).
			Order("available_at ASC").
			Limit(r.opt.Batch).
			Find(&claimed).Error
	})
	if err != nil {
		return 0, err
	}

	published := 0
	for _, m := range claimed {
		mctx := ctx
		if m.TraceID != "" {
			mctx = ctxkit.WithTrace(ctx, m.TraceID)
		}
		perr := r.opt.Publisher.Publish(mctx, m)
		switch {
		case perr == nil:
			now := time.Now().UTC()
			if uerr := r.opt.DB.WithContext(ctx).Model(&Message{}).
				Where("id = ?", m.ID).
				Updates(map[string]any{"published_at": now, "last_error": ""}).Error; uerr != nil {
				// Published but not marked. The next pass republishes it —
				// which is exactly why consumers must be idempotent.
				logx.C(mctx).Error("outbox: published but could not mark; it will be redelivered",
					"id", m.ID.String(), "topic", m.Topic, "error", uerr.Error())
			}
			published++

		case errors.Is(perr, ErrDrop):
			now := time.Now().UTC()
			_ = r.opt.DB.WithContext(ctx).Model(&Message{}).Where("id = ?", m.ID).
				Updates(map[string]any{"published_at": now, "last_error": "dropped: " + perr.Error()}).Error
			logx.C(mctx).Warn("outbox: message dropped", "id", m.ID.String(), "topic", m.Topic)

		default:
			attempts := m.Attempts + 1
			next := time.Now().UTC().Add(r.opt.Backoff << min(attempts-1, 10))
			_ = r.opt.DB.WithContext(ctx).Model(&Message{}).Where("id = ?", m.ID).
				Updates(map[string]any{
					"attempts":     attempts,
					"last_error":   truncate(perr.Error(), 1000),
					"available_at": next,
				}).Error
			if attempts >= r.opt.MaxAttempts {
				logx.C(mctx).Error("outbox: message parked after repeated failures",
					"id", m.ID.String(), "topic", m.Topic, "attempts", attempts, "error", perr.Error())
			}
		}
	}
	return published, nil
}

// Parked returns messages that exhausted their attempts. Alert on this being
// non-empty: it is a side effect that was promised and never happened.
func (r *Relay) Parked(ctx context.Context, limit int) ([]Message, error) {
	var out []Message
	err := r.opt.DB.WithContext(ctx).
		Where("published_at IS NULL AND attempts >= ?", r.opt.MaxAttempts).
		Order("created_at ASC").Limit(limit).Find(&out).Error
	return out, err
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
