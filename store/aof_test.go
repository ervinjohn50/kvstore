package store

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/ervinjohn50/kvstore/resp"
)

func tempAOF(t *testing.T) string {
	return filepath.Join(t.TempDir(), "test.aof")
}

func mustOpen(t *testing.T, path string, maxKeys int) *Store {
	t.Helper()
	s, err := Open(path, maxKeys)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func mustClose(t *testing.T, s *Store) {
	t.Helper()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeCommands(t *testing.T, path string, cmds ...[]string) {
	t.Helper()
	var buf bytes.Buffer
	for _, c := range cmds {
		resp.WriteCommand(&buf, c)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAOFFormat(t *testing.T) {
	path := tempAOF(t)
	s := mustOpen(t, path, 0)
	s.Set("name", "alice")
	s.Get("name") // reads aren't logged
	s.Delete("name")
	mustClose(t, s)

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "*3\r\n$3\r\nSET\r\n$4\r\nname\r\n$5\r\nalice\r\n" + "*2\r\n$3\r\nDEL\r\n$4\r\nname\r\n"
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAOFLogsAbsoluteExpiry(t *testing.T) {
	path := tempAOF(t)
	s := mustOpen(t, path, 0)
	before := time.Now()
	s.SetWithTTL("token", "abc", time.Hour)
	after := time.Now()
	mustClose(t, s)

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	args, err := resp.ReadCommand(bufio.NewReader(f))
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 5 || args[3] != "PXAT" {
		t.Fatalf("expected SET token abc PXAT <ms>, got %q", args)
	}
	ms, _ := strconv.ParseInt(args[4], 10, 64)
	if ms < before.Add(time.Hour).UnixMilli() || ms > after.Add(time.Hour).UnixMilli() {
		t.Fatalf("PXAT %d isn't one hour after the SET", ms)
	}
}

func TestReopenRestoresData(t *testing.T) {
	path := tempAOF(t)
	s := mustOpen(t, path, 0)
	s.Set("a", "1")
	s.Set("b", "2")
	s.Set("b", "updated")
	s.Delete("a")
	s.SetWithTTL("c", "3", time.Hour)
	mustClose(t, s)

	s = mustOpen(t, path, 0)
	defer s.Close()
	if _, ok := s.Get("a"); ok {
		t.Fatal("expected deleted key 'a' to stay deleted")
	}
	if val, ok := s.Get("b"); !ok || val != "updated" {
		t.Fatalf("expected 'updated', got %q (found: %v)", val, ok)
	}
	if val, ok := s.Get("c"); !ok || val != "3" {
		t.Fatalf("expected '3', got %q (found: %v)", val, ok)
	}
}

func TestReplayDropsKeysThatExpiredWhileDown(t *testing.T) {
	path := tempAOF(t)
	past := strconv.FormatInt(time.Now().Add(-time.Second).UnixMilli(), 10)
	future := strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10)
	writeCommands(t, path,
		[]string{"SET", "k", "old"},
		[]string{"SET", "k", "new", "PXAT", past},
		[]string{"SET", "live", "v", "PXAT", future},
	)

	s := mustOpen(t, path, 0)
	defer s.Close()
	if val, ok := s.Get("k"); ok {
		t.Fatalf("expected 'k' to be expired, but got its older value %q", val)
	}
	if _, ok := s.Get("live"); !ok {
		t.Fatal("expected 'live' to exist until its expiry")
	}
}

func TestEvictionsSurviveRestart(t *testing.T) {
	path := tempAOF(t)
	s := mustOpen(t, path, 2)
	s.Set("a", "1")
	s.Set("b", "2")
	s.Get("a")
	s.Set("c", "3") // evicts b, the least recently used
	mustClose(t, s)

	s = mustOpen(t, path, 2)
	defer s.Close()
	expectKeys(t, s, []string{"a", "c"}, []string{"b"})
}

func TestLowerLimitOnReopen(t *testing.T) {
	path := tempAOF(t)
	s := mustOpen(t, path, 0)
	s.Set("a", "1")
	s.Set("b", "2")
	s.Set("c", "3")
	mustClose(t, s)

	s = mustOpen(t, path, 2) // a was written longest ago, so it goes
	expectKeys(t, s, []string{"b", "c"}, []string{"a"})
	mustClose(t, s)

	s = mustOpen(t, path, 0) // that eviction was logged, so a stays gone
	defer s.Close()
	expectKeys(t, s, []string{"b", "c"}, []string{"a"})
}

func TestTruncatedTailIsRepaired(t *testing.T) {
	path := tempAOF(t)
	var buf bytes.Buffer
	resp.WriteCommand(&buf, []string{"SET", "a", "1"})
	buf.WriteString("*3\r\n$3\r\nSET\r\n$1\r\nb") // cut off mid-command, as if by a crash
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	s := mustOpen(t, path, 0)
	s.Set("c", "3")
	mustClose(t, s)

	s = mustOpen(t, path, 0)
	defer s.Close()
	expectKeys(t, s, []string{"a", "c"}, []string{"b"})
}

func TestCorruptFileFailsToOpen(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"not RESP", "this is not RESP\r\n"},
		{"unknown command", "*1\r\n$4\r\nPING\r\n"},
		{"bad expiry", "*5\r\n$3\r\nSET\r\n$1\r\nk\r\n$1\r\nv\r\n$4\r\nPXAT\r\n$4\r\nsoon\r\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tempAOF(t)
			if err := os.WriteFile(path, []byte(tt.content), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := Open(path, 0); err == nil {
				t.Fatal("expected Open to refuse a corrupt file")
			}
		})
	}
}

func TestFailedWriteRefusesChanges(t *testing.T) {
	path := tempAOF(t)
	s := mustOpen(t, path, 0)
	defer s.Close()
	s.Set("a", "1")

	// Simulate the disk failing by closing the file underneath the store.
	s.aof.mu.Lock()
	s.aof.f.Close()
	s.aof.mu.Unlock()

	if err := s.Set("b", "2"); err == nil {
		t.Fatal("expected Set to fail when the file can't be written")
	}
	if _, ok := s.Get("b"); ok {
		t.Fatal("a failed write shouldn't change memory")
	}
	if _, err := s.Delete("a"); err == nil {
		t.Fatal("expected writes to keep failing after the first error")
	}
	if val, ok := s.Get("a"); !ok || val != "1" {
		t.Fatal("reads should still work after a write failure")
	}
}

func TestClosedStoreRefusesChanges(t *testing.T) {
	s := mustOpen(t, tempAOF(t), 0)
	mustClose(t, s)
	if err := s.Set("a", "1"); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("expected a closed-file error, got %v", err)
	}
}
