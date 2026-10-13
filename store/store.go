package store

import (
	"container/list"
	"sync"
	"time"
)

type entry struct {
	key       string // stored here too, so eviction can find the map entry to delete
	value     string
	expiresAt time.Time // zero means no expiration
}

func (e *entry) expired() bool {
	return !e.expiresAt.IsZero() && time.Now().After(e.expiresAt)
}

// Store is a thread-safe in-memory key-value store with optional TTL support.
// It uses lazy expiration. Expired keys are only cleaned up when accessed,
// avoiding the overhead of a background cleanup goroutine.
//
// With a key limit, it evicts the least recently used key to make room for a
// new one. The map finds a key's node in a linked list ordered by use, so
// lookups, moving a key to the front, and evicting from the back are all O(1).
type Store struct {
	mu      sync.Mutex
	maxKeys int                      // 0 means no limit
	items   map[string]*list.Element // each Element's Value is an *entry
	order   *list.List               // most recently used at the front
}

// New returns an empty Store that holds at most maxKeys keys, or any number if maxKeys is 0.
func New(maxKeys int) *Store {
	return &Store{
		maxKeys: maxKeys,
		items:   make(map[string]*list.Element),
		order:   list.New(),
	}
}

// Set stores a key with no expiration.
func (s *Store) Set(key, value string) {
	s.set(key, value, time.Time{})
}

// SetWithTTL stores a key that expires after the given duration.
func (s *Store) SetWithTTL(key, value string, ttl time.Duration) {
	s.set(key, value, time.Now().Add(ttl))
}

func (s *Store) set(key, value string, expiresAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if el, ok := s.items[key]; ok {
		e := el.Value.(*entry)
		e.value, e.expiresAt = value, expiresAt
		s.order.MoveToFront(el)
		return
	}

	s.items[key] = s.order.PushFront(&entry{key: key, value: value, expiresAt: expiresAt})
	if s.maxKeys > 0 && s.order.Len() > s.maxKeys {
		s.remove(s.order.Back())
	}
}

// Get retrieves a value by key and marks it as recently used.
// Returns false if the key doesn't exist or has expired.
func (s *Store) Get(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	el, ok := s.items[key]
	if !ok {
		return "", false
	}

	e := el.Value.(*entry)
	if e.expired() {
		s.remove(el)
		return "", false
	}

	s.order.MoveToFront(el)
	return e.value, true
}

// Delete removes a key and reports whether it existed.
// An expired key counts as already gone.
func (s *Store) Delete(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	el, ok := s.items[key]
	if !ok {
		return false
	}
	s.remove(el)
	return !el.Value.(*entry).expired()
}

// remove deletes a key from both the list and the map. The caller must hold s.mu.
func (s *Store) remove(el *list.Element) {
	s.order.Remove(el)
	delete(s.items, el.Value.(*entry).key)
}
