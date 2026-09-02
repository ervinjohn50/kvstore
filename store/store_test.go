package store

import "testing"

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
