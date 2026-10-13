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
//
// A Store from Open also records every change in an append-only file.
type Store struct {
	mu      sync.Mutex
	maxKeys int                      // 0 means no limit
	items   map[string]*list.Element // each Element's Value is an *entry
	order   *list.List               // most recently used at the front
	aof     *aof                     // nil for an in-memory-only Store
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
// It only fails if the change can't be written to the append-only file.
func (s *Store) Set(key, value string) error {
	return s.set(key, value, time.Time{})
}

// SetWithTTL stores a key that expires after the given duration.
// It only fails if the change can't be written to the append-only file.
func (s *Store) SetWithTTL(key, value string, ttl time.Duration) error {
	return s.set(key, value, time.Now().Add(ttl))
}

func (s *Store) set(key, value string, expiresAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	el, exists := s.items[key]
	var victim *list.Element
	if !exists && s.maxKeys > 0 && s.order.Len() >= s.maxKeys {
		victim = s.order.Back()
	}

	// Log before changing memory, so a failed disk write leaves the Store as it was.
	if s.aof != nil {
		if err := s.aof.logSet(key, value, expiresAt); err != nil {
			return err
		}
		if victim != nil {
			if err := s.aof.logDelete(victim.Value.(*entry).key); err != nil {
				return err
			}
		}
	}

	if exists {
		e := el.Value.(*entry)
		e.value, e.expiresAt = value, expiresAt
		s.order.MoveToFront(el)
	} else {
		s.items[key] = s.order.PushFront(&entry{key: key, value: value, expiresAt: expiresAt})
	}
	if victim != nil {
		s.remove(victim)
	}
	return nil
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
// It only fails if the change can't be written to the append-only file.
func (s *Store) Delete(key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	el, ok := s.items[key]
	if !ok {
		return false, nil
	}
	if s.aof != nil {
		if err := s.aof.logDelete(key); err != nil {
			return false, err
		}
	}
	s.remove(el)
	return !el.Value.(*entry).expired(), nil
}

// Close syncs and closes the append-only file of a Store from Open, after
// which changes fail. It does nothing for an in-memory Store.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.aof == nil {
		return nil
	}
	return s.aof.close()
}

// remove deletes a key from both the list and the map. The caller must hold s.mu.
func (s *Store) remove(el *list.Element) {
	s.order.Remove(el)
	delete(s.items, el.Value.(*entry).key)
}
