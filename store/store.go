package store

import (
	"sync"
	"time"
)

type entry struct {
	value     string
	expiresAt time.Time // zero means no expiration
}

func (e entry) expired() bool {
	return !e.expiresAt.IsZero() && time.Now().After(e.expiresAt)
}

// Store is a thread-safe in-memory key-value store with optional TTL support.
// It uses lazy expiration. Expired keys are only cleaned up when accessed,
// avoiding the overhead of a background cleanup goroutine.
type Store struct {
	mu   sync.Mutex
	data map[string]entry
}

// New returns an empty Store.
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
	}
}

// Get retrieves a value by key. Returns false if the key doesn't exist or has expired.
func (s *Store) Get(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.data[key]
	if !ok {
		return "", false
	}

	if e.expired() {
		delete(s.data, key)
		return "", false
	}

	return e.value, true
}

// Delete removes a key and reports whether it existed.
// An expired key counts as already gone.
func (s *Store) Delete(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.data[key]
	if !ok {
		return false
	}
	delete(s.data, key)
	return !e.expired()
}
