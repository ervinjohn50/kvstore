package store

import (
	"testing"
	"time"
)

func TestSetAndGet(t *testing.T) {
	s := New()
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
	s := New()

	_, ok := s.Get("missing")
	if ok {
		t.Fatal("expected key 'missing' to not exist")
	}
}

func TestDelete(t *testing.T) {
	s := New()
	s.Set("name", "alice")
	s.Delete("name")

	_, ok := s.Get("name")
	if ok {
		t.Fatal("expected key 'name' to be deleted")
	}
}

func TestDeleteMissing(t *testing.T) {
	s := New()
	s.Delete("missing") // should not panic
}

func TestOverwrite(t *testing.T) {
	s := New()
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
	s := New()
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
	s := New()
	s.SetWithTTL("token", "abc123", 50*time.Millisecond)

	time.Sleep(100 * time.Millisecond)

	_, ok := s.Get("token")
	if ok {
		t.Fatal("expected key 'token' to be expired")
	}
}

func TestSetWithoutTTLNeverExpires(t *testing.T) {
	s := New()
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
