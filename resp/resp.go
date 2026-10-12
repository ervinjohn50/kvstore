// Package resp reads client commands and writes replies in RESP, the Redis
// Serialization Protocol: https://redis.io/docs/latest/develop/reference/protocol-spec/
package resp

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
)

// Limits keep a single malicious or buggy client from making the server
// allocate huge amounts of memory with a tiny request like "$999999999".
const (
	maxArgs    = 1024
	maxBulkLen = 1 << 20 // 1 MiB
)

// ErrProtocol means the client sent bytes that aren't valid RESP.
var ErrProtocol = errors.New("protocol error")

// ReadCommand reads one command, sent as an array of bulk strings
// (e.g. "*2\r\n$3\r\nGET\r\n$4\r\nname\r\n" becomes ["GET", "name"]).
// It returns io.EOF if the client disconnected cleanly between commands.
func ReadCommand(r *bufio.Reader) ([]string, error) {
	line, err := readLine(r)
	if err != nil {
		return nil, err
	}
	if len(line) == 0 || line[0] != '*' {
		return nil, fmt.Errorf("%w: expected '*', got %q", ErrProtocol, line)
	}
	n, err := strconv.Atoi(line[1:])
	if err != nil || n < 1 || n > maxArgs {
		return nil, fmt.Errorf("%w: invalid array length %q", ErrProtocol, line[1:])
	}

	args := make([]string, n)
	for i := range args {
		args[i], err = readBulkString(r)
		if err == io.EOF {
			// The client hung up mid-command, which isn't a clean disconnect.
			err = io.ErrUnexpectedEOF
		}
		if err != nil {
			return nil, err
		}
	}
	return args, nil
}

// readBulkString reads by length instead of scanning for "\r\n", so values
// can safely contain "\r\n" themselves (RESP is "binary safe").
func readBulkString(r *bufio.Reader) (string, error) {
	line, err := readLine(r)
	if err != nil {
		return "", err
	}
	if len(line) == 0 || line[0] != '$' {
		return "", fmt.Errorf("%w: expected '$', got %q", ErrProtocol, line)
	}
	size, err := strconv.Atoi(line[1:])
	if err != nil || size < 0 || size > maxBulkLen {
		return "", fmt.Errorf("%w: invalid bulk length %q", ErrProtocol, line[1:])
	}

	buf := make([]byte, size+2)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	if buf[size] != '\r' || buf[size+1] != '\n' {
		return "", fmt.Errorf("%w: bulk string not terminated by CRLF", ErrProtocol)
	}
	return string(buf[:size]), nil
}

// readLine reads a header line like "*3" or "$5" without its "\r\n".
// ReadSlice fails once a line outgrows bufio's buffer (4 KiB by default),
// so a client can't stream endless bytes without a newline.
func readLine(r *bufio.Reader) (string, error) {
	b, err := r.ReadSlice('\n')
	if err != nil {
		if errors.Is(err, bufio.ErrBufferFull) {
			return "", fmt.Errorf("%w: line too long", ErrProtocol)
		}
		if err == io.EOF && len(b) > 0 {
			return "", io.ErrUnexpectedEOF
		}
		return "", err
	}
	if len(b) < 2 || b[len(b)-2] != '\r' {
		return "", fmt.Errorf("%w: line not terminated by CRLF", ErrProtocol)
	}
	return string(b[:len(b)-2]), nil
}

// WriteSimpleString writes a short status reply like "+OK".
func WriteSimpleString(w io.Writer, s string) error {
	_, err := fmt.Fprintf(w, "+%s\r\n", s)
	return err
}

// WriteError writes an error reply like "-ERR unknown command".
func WriteError(w io.Writer, msg string) error {
	_, err := fmt.Fprintf(w, "-%s\r\n", msg)
	return err
}

// WriteInteger writes a number reply like ":1".
func WriteInteger(w io.Writer, n int64) error {
	_, err := fmt.Fprintf(w, ":%d\r\n", n)
	return err
}

// WriteBulkString writes a length-prefixed value like "$5\r\nalice".
func WriteBulkString(w io.Writer, s string) error {
	_, err := fmt.Fprintf(w, "$%d\r\n%s\r\n", len(s), s)
	return err
}

// WriteNull writes the "no value" reply, used when a key doesn't exist.
func WriteNull(w io.Writer) error {
	_, err := io.WriteString(w, "$-1\r\n")
	return err
}
