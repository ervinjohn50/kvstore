package resp

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
)

func reader(s string) *bufio.Reader {
	return bufio.NewReader(strings.NewReader(s))
}

func TestReadCommand(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"single arg", "*1\r\n$4\r\nPING\r\n", []string{"PING"}},
		{"multiple args", "*3\r\n$3\r\nSET\r\n$4\r\nname\r\n$5\r\nalice\r\n", []string{"SET", "name", "alice"}},
		{"empty string", "*2\r\n$3\r\nGET\r\n$0\r\n\r\n", []string{"GET", ""}},
		{"value containing CRLF", "*2\r\n$3\r\nGET\r\n$4\r\na\r\nb\r\n", []string{"GET", "a\r\nb"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ReadCommand(reader(tt.input))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReadCommandSequential(t *testing.T) {
	r := reader("*1\r\n$4\r\nPING\r\n*2\r\n$3\r\nGET\r\n$1\r\nk\r\n")

	first, err := ReadCommand(r)
	if err != nil || !slices.Equal(first, []string{"PING"}) {
		t.Fatalf("first command: got %q, %v", first, err)
	}
	second, err := ReadCommand(r)
	if err != nil || !slices.Equal(second, []string{"GET", "k"}) {
		t.Fatalf("second command: got %q, %v", second, err)
	}
	if _, err := ReadCommand(r); err != io.EOF {
		t.Fatalf("expected io.EOF after last command, got %v", err)
	}
}

func TestReadCommandProtocolErrors(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"not an array", "PING\r\n"},
		{"empty line", "\r\n"},
		{"bad array length", "*abc\r\n"},
		{"zero args", "*0\r\n"},
		{"too many args", "*99999\r\n"},
		{"arg not a bulk string", "*1\r\n+PING\r\n"},
		{"negative bulk length", "*1\r\n$-5\r\n"},
		{"bulk length too large", "*1\r\n$999999999\r\n"},
		{"missing CR", "*1\n$4\nPING\n"},
		{"bulk string too long for its length", "*1\r\n$2\r\nPING\r\n"},
		{"line too long", "*" + strings.Repeat("1", 5000) + "\r\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ReadCommand(reader(tt.input))
			if !errors.Is(err, ErrProtocol) {
				t.Fatalf("expected ErrProtocol, got %v", err)
			}
		})
	}
}

func TestReadCommandTruncated(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"mid header", "*2"},
		{"missing second arg", "*2\r\n$3\r\nGET\r\n"},
		{"bulk header without body", "*1\r\n$4\r\n"},
		{"mid bulk string", "*1\r\n$4\r\nPI"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ReadCommand(reader(tt.input))
			if err == nil || err == io.EOF {
				t.Fatalf("expected an unexpected-EOF error, got %v", err)
			}
		})
	}
}

func TestWriters(t *testing.T) {
	tests := []struct {
		name  string
		write func(io.Writer) error
		want  string
	}{
		{"simple string", func(w io.Writer) error { return WriteSimpleString(w, "OK") }, "+OK\r\n"},
		{"error", func(w io.Writer) error { return WriteError(w, "ERR boom") }, "-ERR boom\r\n"},
		{"integer", func(w io.Writer) error { return WriteInteger(w, 42) }, ":42\r\n"},
		{"bulk string", func(w io.Writer) error { return WriteBulkString(w, "alice") }, "$5\r\nalice\r\n"},
		{"empty bulk string", func(w io.Writer) error { return WriteBulkString(w, "") }, "$0\r\n\r\n"},
		{"null", WriteNull, "$-1\r\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := tt.write(&buf); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if buf.String() != tt.want {
				t.Fatalf("got %q, want %q", buf.String(), tt.want)
			}
		})
	}
}
