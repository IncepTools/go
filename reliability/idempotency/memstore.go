package idempotency

import (
	"context"
	"sync"
	"time"
)

// MemoryStore keeps records in process memory.
//
// For tests and single-instance development only. It is NOT a production
// store for two reasons, and both of them defeat the purpose of the package:
// a restart loses every record, and each replica has its own map, so the
// retry that lands on a different replica executes again.
type MemoryStore struct {
	mu      sync.Mutex
	records map[string]*GormRecord
}

// NewMemoryStore returns an in-process store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{records: map[string]*GormRecord{}}
}

// Begin implements Store.
func (s *MemoryStore) Begin(_ context.Context, key, fingerprint string, ttl time.Duration) (*Record, error) {
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.records[key]
	if !ok || existing.ExpiresAt.Before(now) {
		s.records[key] = &GormRecord{
			Key: key, Fingerprint: fingerprint, CreatedAt: now, ExpiresAt: now.Add(ttl),
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
func (s *MemoryStore) Complete(_ context.Context, key string, rec Record, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[key]
	if !ok {
		return nil
	}
	r.Status, r.Body, r.ContentType = rec.Status, rec.Body, rec.ContentType
	r.Completed = true
	r.ExpiresAt = time.Now().UTC().Add(ttl)
	return nil
}

// Abandon implements Store.
func (s *MemoryStore) Abandon(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.records[key]; ok && !r.Completed {
		delete(s.records, key)
	}
	return nil
}
