package store

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/ervinjohn50/kvstore/resp"
)

// Open returns a Store backed by the append-only file at path, creating the
// file if needed. Every change is appended to it as a RESP command, the same
// format clients send, and Open replays those commands to rebuild the data:
//
//	SET key value                 a key with no expiration
//	SET key value PXAT <unix ms>  a key that expires at an absolute time,
//	                              so a restart doesn't reset its TTL
//	DEL key                       a deleted or evicted key
//
// A change reaches the operating system before Set or Delete returns, so it
// survives the process crashing. The file is synced to disk every second, so a
// power loss can lose about the last second of changes (like Redis's
// "appendfsync everysec").
func Open(path string, maxKeys int) (*Store, error) {
	// Replay with no limit. Reads aren't logged, so replaying with the limit
	// could evict different keys than the original run did; the file already
	// has a DEL for every real eviction.
	s := New(0)
	if err := s.replay(path); err != nil {
		return nil, err
	}

	a, err := openAOF(path)
	if err != nil {
		return nil, err
	}
	s.aof = a
	s.maxKeys = maxKeys

	// If the limit is lower than last time, evict down to it, logging each eviction.
	for maxKeys > 0 && s.order.Len() > maxKeys {
		victim := s.order.Back()
		if err := a.logDelete(victim.Value.(*entry).key); err != nil {
			a.close()
			return nil, err
		}
		s.remove(victim)
	}
	return s, nil
}

func (s *Store) replay(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	cr := &countingReader{r: f}
	r := bufio.NewReader(cr)
	var validLen int64 // bytes of complete commands read so far

	for {
		args, err := resp.ReadCommand(r)
		if err == io.EOF {
			return nil
		}
		if errors.Is(err, io.ErrUnexpectedEOF) {
			// A crash mid-write can leave a partial command at the end. Cut it
			// off, or the next command appended after it couldn't be read back.
			log.Printf("%s: removing incomplete command at end of file (offset %d)", path, validLen)
			return os.Truncate(path, validLen)
		}
		if err != nil {
			return fmt.Errorf("%s at offset %d: %w", path, validLen, err)
		}
		if err := s.apply(args); err != nil {
			return fmt.Errorf("%s at offset %d: %w", path, validLen, err)
		}
		validLen = cr.n - int64(r.Buffered())
	}
}

// apply replays one logged command. s.aof is still nil, so nothing gets logged again.
func (s *Store) apply(args []string) error {
	switch {
	case len(args) == 3 && args[0] == "SET":
		return s.Set(args[1], args[2])

	case len(args) == 5 && args[0] == "SET" && args[3] == "PXAT":
		ms, err := strconv.ParseInt(args[4], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid PXAT time %q", args[4])
		}
		expiresAt := time.UnixMilli(ms)
		if time.Now().After(expiresAt) {
			// It expired while the server was down. Deleting instead of skipping
			// matters if an earlier SET in the file gave this key another value.
			_, err := s.Delete(args[1])
			return err
		}
		return s.set(args[1], args[2], expiresAt)

	case len(args) == 2 && args[0] == "DEL":
		_, err := s.Delete(args[1])
		return err

	default:
		return fmt.Errorf("unexpected command %q", args)
	}
}

// countingReader counts bytes read, so replay knows where the last complete command ended.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// aof appends changes to the file and syncs it to disk in the background.
type aof struct {
	mu      sync.Mutex // guards f and err between writes and the background sync
	f       *os.File
	err     error // the first write or sync failure; every later write returns it
	done    chan struct{}
	stopped chan struct{}
}

func openAOF(path string) (*aof, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	a := &aof{f: f, done: make(chan struct{}), stopped: make(chan struct{})}
	go a.syncEverySecond()
	return a, nil
}

func (a *aof) logSet(key, value string, expiresAt time.Time) error {
	args := []string{"SET", key, value}
	if !expiresAt.IsZero() {
		args = append(args, "PXAT", strconv.FormatInt(expiresAt.UnixMilli(), 10))
	}
	return a.write(args)
}

func (a *aof) logDelete(key string) error {
	return a.write([]string{"DEL", key})
}

// write appends one command with a single write call. After any failure it
// refuses all writes, since the file may now end with a partial command.
func (a *aof) write(args []string) error {
	var buf bytes.Buffer
	resp.WriteCommand(&buf, args)

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.err != nil {
		return a.err
	}
	if _, err := a.f.Write(buf.Bytes()); err != nil {
		a.err = fmt.Errorf("writing append-only file: %w", err)
	}
	return a.err
}

func (a *aof) syncEverySecond() {
	defer close(a.stopped)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			a.sync()
		case <-a.done:
			return
		}
	}
}

func (a *aof) sync() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.err != nil {
		return a.err
	}
	if err := a.f.Sync(); err != nil {
		a.err = fmt.Errorf("syncing append-only file: %w", err)
	}
	return a.err
}

func (a *aof) close() error {
	close(a.done)
	<-a.stopped
	return errors.Join(a.sync(), a.f.Close())
}
