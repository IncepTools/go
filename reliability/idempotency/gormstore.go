package idempotency

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GormRecord is the table behind GormStore.
//
// Create it with a versioned migration, not AutoMigrate:
//
//	CREATE TABLE idempotency_records (
//	    key          TEXT PRIMARY KEY,
//	    fingerprint  TEXT NOT NULL,
//	    status       INTEGER NOT NULL DEFAULT 0,
//	    body         BYTEA,
//	    content_type TEXT,
//	    completed    BOOLEAN NOT NULL DEFAULT FALSE,
//	    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
//	    expires_at   TIMESTAMPTZ NOT NULL
//	);
//	CREATE INDEX idempotency_records_expires_at_idx ON idempotency_records (expires_at);
type GormRecord struct {
	Key         string `gorm:"primaryKey;column:key"`
	Fingerprint string `gorm:"column:fingerprint"`
	Status      int    `gorm:"column:status"`
	Body        []byte `gorm:"column:body"`
	ContentType string `gorm:"column:content_type"`
	// Completed separates a reservation from a recorded response. A row that
	// exists but is not completed means "in flight".
	Completed bool      `gorm:"column:completed"`
	CreatedAt time.Time `gorm:"column:created_at"`
	ExpiresAt time.Time `gorm:"column:expires_at"`
}

// TableName implements gorm's Tabler.
func (GormRecord) TableName() string { return "idempotency_records" }

// GormStore keeps records in the database.
//
// The database, not Redis, because the record must survive the restart the
// client is retrying through — a cache that was evicted is a duplicate
// charge. Redis is the right choice only when it is configured to persist.
type GormStore struct{ db *gorm.DB }

// NewGormStore builds a store.
func NewGormStore(db *gorm.DB) *GormStore { return &GormStore{db: db} }

// Begin implements Store.
//
// The reservation is a single INSERT ... ON CONFLICT DO NOTHING, so two
// concurrent first-requests cannot both win: exactly one inserts a row, and
// the other reads what that one wrote. Doing it as SELECT-then-INSERT would
// leave the race open in the window between them, which is precisely the
// window a retrying client occupies.
func (s *GormStore) Begin(ctx context.Context, key, fingerprint string, ttl time.Duration) (*Record, error) {
	now := time.Now().UTC()
	row := GormRecord{
		Key:         key,
		Fingerprint: fingerprint,
		CreatedAt:   now,
		ExpiresAt:   now.Add(ttl),
	}

	res := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "key"}}, DoNothing: true}).
		Create(&row)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 1 {
		return nil, nil // we own the key
	}

	var existing GormRecord
	if err := s.db.WithContext(ctx).First(&existing, "key = ?", key).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// The row vanished between the conflict and the read — expired
			// and swept. Treat it as ours; the worst case is one extra
			// execution of a request the client had given up on.
			return nil, nil
		}
		return nil, err
	}
	if existing.ExpiresAt.Before(now) {
		// Expired. Take it over rather than replaying something stale.
		if err := s.db.WithContext(ctx).Model(&GormRecord{}).Where("key = ?", key).
			Updates(map[string]any{
				"fingerprint": fingerprint,
				"completed":   false,
				"status":      0,
				"body":        nil,
				"created_at":  now,
				"expires_at":  now.Add(ttl),
			}).Error; err != nil {
			return nil, err
		}
		return nil, nil
	}
	if !existing.Completed {
		return nil, ErrInFlight
	}
	return &Record{
		Key:         existing.Key,
		Fingerprint: existing.Fingerprint,
		Status:      existing.Status,
		Body:        existing.Body,
		ContentType: existing.ContentType,
		CreatedAt:   existing.CreatedAt,
	}, nil
}

// Complete implements Store.
func (s *GormStore) Complete(ctx context.Context, key string, rec Record, ttl time.Duration) error {
	return s.db.WithContext(ctx).Model(&GormRecord{}).Where("key = ?", key).
		Updates(map[string]any{
			"status":       rec.Status,
			"body":         rec.Body,
			"content_type": rec.ContentType,
			"completed":    true,
			"expires_at":   time.Now().UTC().Add(ttl),
		}).Error
}

// Abandon implements Store.
func (s *GormStore) Abandon(ctx context.Context, key string) error {
	// Only an uncompleted reservation is removed: a completed record must
	// survive, or a later retry re-executes a request that already succeeded.
	return s.db.WithContext(ctx).
		Where("key = ? AND completed = ?", key, false).
		Delete(&GormRecord{}).Error
}

// Sweep deletes expired records. Run it from a scheduled job; without it the
// table grows by one row per write request, forever.
func (s *GormStore) Sweep(ctx context.Context) (int64, error) {
	res := s.db.WithContext(ctx).Where("expires_at < ?", time.Now().UTC()).Delete(&GormRecord{})
	return res.RowsAffected, res.Error
}
