package store

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// expectKeys checks which keys exist. Note that Get marks keys as recently used.
func expectKeys(t *testing.T, s *Store, present, missing []string) {
	t.Helper()
	for _, k := range present {
		if _, ok := s.Get(k); !ok {
			t.Fatalf("expected key %q to exist", k)
		}
	}
	for _, k := range missing {
		if _, ok := s.Get(k); ok {
			t.Fatalf("expected key %q to be evicted", k)
		}
	}
}

func TestSetAndGet(t *testing.T) {
	s := New(0)
	s.Set("name", "alice")

	val, ok := s.Get("name")
	if !ok {
		t.Fatal("expected key 'name' to exist")
	}
	if val != "alice" {
		t.Fatalf("expected 'alice', got '%s'", val)
	}
}

func TestGetMissing(t *testing.T) {
	s := New(0)

	_, ok := s.Get("missing")
	if ok {
		t.Fatal("expected key 'missing' to not exist")
	}
}

func TestDelete(t *testing.T) {
	s := New(0)
	s.Set("name", "alice")

	if deleted, err := s.Delete("name"); err != nil || !deleted {
		t.Fatalf("expected Delete to report that 'name' existed, got %v, %v", deleted, err)
	}
	if _, ok := s.Get("name"); ok {
		t.Fatal("expected key 'name' to be deleted")
	}
}

func TestDeleteMissing(t *testing.T) {
	s := New(0)
	if deleted, err := s.Delete("missing"); err != nil || deleted {
		t.Fatalf("expected Delete to report that 'missing' didn't exist, got %v, %v", deleted, err)
	}
}

func TestDeleteExpired(t *testing.T) {
	s := New(0)
	s.SetWithTTL("token", "abc123", 10*time.Millisecond)
	time.Sleep(20 * time.Millisecond)

	if deleted, err := s.Delete("token"); err != nil || deleted {
		t.Fatalf("expected Delete to treat an expired key as missing, got %v, %v", deleted, err)
	}
}

func TestOverwrite(t *testing.T) {
	s := New(0)
	s.Set("name", "alice")
	s.Set("name", "bob")

	val, ok := s.Get("name")
	if !ok {
		t.Fatal("expected key 'name' to exist")
	}
	if val != "bob" {
		t.Fatalf("expected 'bob', got '%s'", val)
	}
}

func TestSetWithTTL(t *testing.T) {
	s := New(0)
	s.SetWithTTL("token", "abc123", 1*time.Second)

	val, ok := s.Get("token")
	if !ok {
		t.Fatal("expected key 'token' to exist before expiry")
	}
	if val != "abc123" {
		t.Fatalf("expected 'abc123', got '%s'", val)
	}
}

func TestKeyExpires(t *testing.T) {
	s := New(0)
	s.SetWithTTL("token", "abc123", 50*time.Millisecond)

	time.Sleep(100 * time.Millisecond)

	_, ok := s.Get("token")
	if ok {
		t.Fatal("expected key 'token' to be expired")
	}
}

func TestSetWithoutTTLNeverExpires(t *testing.T) {
	s := New(0)
	s.Set("name", "alice")

	time.Sleep(50 * time.Millisecond)

	val, ok := s.Get("name")
	if !ok {
		t.Fatal("expected key 'name' to still exist")
	}
	if val != "alice" {
		t.Fatalf("expected 'alice', got '%s'", val)
	}
}

func TestSetClearsPreviousTTL(t *testing.T) {
	s := New(0)
	s.SetWithTTL("name", "alice", 10*time.Millisecond)
	s.Set("name", "bob")

	time.Sleep(20 * time.Millisecond)

	if val, ok := s.Get("name"); !ok || val != "bob" {
		t.Fatalf("expected 'bob' with no expiration, got %q (found: %v)", val, ok)
	}
}

func TestEvictsLeastRecentlyUsed(t *testing.T) {
	s := New(2)
	s.Set("a", "1")
	s.Set("b", "2")
	s.Set("c", "3")

	expectKeys(t, s, []string{"b", "c"}, []string{"a"})
}

func TestGetMarksKeyAsRecentlyUsed(t *testing.T) {
	s := New(2)
	s.Set("a", "1")
	s.Set("b", "2")
	s.Get("a")
	s.Set("c", "3")

	expectKeys(t, s, []string{"a", "c"}, []string{"b"})
}

func TestOverwriteMarksKeyAsRecentlyUsed(t *testing.T) {
	s := New(2)
	s.Set("a", "1")
	s.Set("b", "2")
	s.Set("a", "updated") // shouldn't count as a new key or evict anything
	expectKeys(t, s, []string{"a", "b"}, nil)

	s.Set("c", "3")
	expectKeys(t, s, []string{"b", "c"}, []string{"a"})
}

func TestDeleteFreesSpace(t *testing.T) {
	s := New(2)
	s.Set("a", "1")
	s.Set("b", "2")
	s.Delete("a")
	s.Set("c", "3")

	expectKeys(t, s, []string{"b", "c"}, []string{"a"})
}

func TestNoLimit(t *testing.T) {
	s := New(0)
	for i := range 1000 {
		s.Set(fmt.Sprint(i), "v")
	}
	for i := range 1000 {
		if _, ok := s.Get(fmt.Sprint(i)); !ok {
			t.Fatalf("expected key %d to exist with no limit", i)
		}
	}
}

func TestConcurrentAccessWithLimit(t *testing.T) {
	s := New(10)
	var wg sync.WaitGroup
	for g := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 200 {
				key := fmt.Sprintf("g%d-k%d", g, i%15)
				s.Set(key, "v")
				s.Get(key)
				if i%7 == 0 {
					s.Delete(key)
				}
			}
		}()
	}
	wg.Wait()

	// The map and the list must still agree, and the limit must hold.
	if len(s.items) != s.order.Len() {
		t.Fatalf("map has %d keys but list has %d", len(s.items), s.order.Len())
	}
	if s.order.Len() > 10 {
		t.Fatalf("expected at most 10 keys, got %d", s.order.Len())
	}
}
