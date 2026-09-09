package store

import (
	"sync"
	"time"
)

type entry struct {
	value     string
	expiresAt time.Time
	// hasTTL distinguishes "no expiration" from "expires at zero time"
	hasTTL bool
}

// Store is a thread-safe in-memory key-value store with optional TTL support.
// It uses lazy expiration. expired keys are only cleaned up when accessed,
// avoiding the overhead of a background cleanup goroutine.
type Store struct {
	// RWMutex allows concurrent reads but exclusive writes
	mu   sync.RWMutex
	data map[string]entry
}

func New() *Store {
	return &Store{
		data: make(map[string]entry),
	}
}

// Set stores a key with no expiration.
func (s *Store) Set(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = entry{value: value}
}

// SetWithTTL stores a key that expires after the given duration.
func (s *Store) SetWithTTL(key, value string, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = entry{
		value:     value,
		expiresAt: time.Now().Add(ttl),
		hasTTL:    true,
	}
}

// Get retrieves a value by key. Returns false if the key doesn't exist or has expired.
// Uses a write lock instead of a read lock because it may delete expired keys (lazy expiration).
func (s *Store) Get(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.data[key]
	if !ok {
		return "", false
	}

	if e.hasTTL && time.Now().After(e.expiresAt) {
		delete(s.data, key)
		return "", false
	}

	return e.value, true
}

func (s *Store) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
}
