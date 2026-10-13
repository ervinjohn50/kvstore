package server

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/ervinjohn50/kvstore/store"
)

// startServer runs a server on a random free port and returns its address.
func startServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go New(store.New(0)).Serve(ln)
	return ln.Addr().String()
}

type client struct {
	t    *testing.T
	conn net.Conn
	r    *bufio.Reader
}

func dial(t *testing.T, addr string) *client {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	// Fail the test instead of hanging forever if the server never replies.
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	return &client{t: t, conn: conn, r: bufio.NewReader(conn)}
}

// do sends a command and returns the server's raw reply.
func (c *client) do(args ...string) string {
	c.t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "*%d\r\n", len(args))
	for _, a := range args {
		fmt.Fprintf(&b, "$%d\r\n%s\r\n", len(a), a)
	}
	c.send(b.String())
	return c.readReply()
}

func (c *client) send(raw string) {
	c.t.Helper()
	if _, err := io.WriteString(c.conn, raw); err != nil {
		c.t.Fatal(err)
	}
}

// readReply reads one reply. A bulk string reply spans two lines.
func (c *client) readReply() string {
	c.t.Helper()
	line, err := c.r.ReadString('\n')
	if err != nil {
		c.t.Fatalf("reading reply: %v", err)
	}
	if line[0] == '$' && line != "$-1\r\n" {
		body, err := c.r.ReadString('\n')
		if err != nil {
			c.t.Fatalf("reading bulk body: %v", err)
		}
		line += body
	}
	return line
}

func expect(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestPing(t *testing.T) {
	c := dial(t, startServer(t))
	expect(t, c.do("PING"), "+PONG\r\n")
	expect(t, c.do("PING", "hello"), "$5\r\nhello\r\n")
}

func TestSetAndGet(t *testing.T) {
	c := dial(t, startServer(t))
	expect(t, c.do("SET", "name", "alice"), "+OK\r\n")
	expect(t, c.do("GET", "name"), "$5\r\nalice\r\n")
	expect(t, c.do("GET", "missing"), "$-1\r\n")
}

func TestCommandsAreCaseInsensitive(t *testing.T) {
	c := dial(t, startServer(t))
	expect(t, c.do("set", "name", "alice"), "+OK\r\n")
	expect(t, c.do("Get", "name"), "$5\r\nalice\r\n")
}

func TestSetWithEX(t *testing.T) {
	c := dial(t, startServer(t))
	expect(t, c.do("SET", "token", "abc", "EX", "100"), "+OK\r\n")
	expect(t, c.do("SET", "other", "xyz", "ex", "100"), "+OK\r\n")
	expect(t, c.do("GET", "token"), "$3\r\nabc\r\n")
}

func TestDel(t *testing.T) {
	c := dial(t, startServer(t))
	c.do("SET", "a", "1")
	c.do("SET", "b", "2")
	expect(t, c.do("DEL", "a", "b", "missing"), ":2\r\n")
	expect(t, c.do("GET", "a"), "$-1\r\n")
}

func TestCommandErrors(t *testing.T) {
	c := dial(t, startServer(t))
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"FOO"}, "-ERR unknown command \"FOO\"\r\n"},
		{[]string{"PING", "a", "b"}, "-ERR wrong number of arguments for 'ping' command\r\n"},
		{[]string{"GET"}, "-ERR wrong number of arguments for 'get' command\r\n"},
		{[]string{"GET", "a", "b"}, "-ERR wrong number of arguments for 'get' command\r\n"},
		{[]string{"SET", "k"}, "-ERR wrong number of arguments for 'set' command\r\n"},
		{[]string{"DEL"}, "-ERR wrong number of arguments for 'del' command\r\n"},
		{[]string{"SET", "k", "v", "EX"}, "-ERR syntax error\r\n"},
		{[]string{"SET", "k", "v", "PX", "10"}, "-ERR syntax error\r\n"},
		{[]string{"SET", "k", "v", "EX", "0"}, "-ERR invalid expire time in 'set' command\r\n"},
		{[]string{"SET", "k", "v", "EX", "-1"}, "-ERR invalid expire time in 'set' command\r\n"},
		{[]string{"SET", "k", "v", "EX", "abc"}, "-ERR invalid expire time in 'set' command\r\n"},
		{[]string{"SET", "k", "v", "EX", "99999999999"}, "-ERR invalid expire time in 'set' command\r\n"},
	}
	for _, tt := range tests {
		expect(t, c.do(tt.args...), tt.want)
		// A command error shouldn't break the connection.
		expect(t, c.do("PING"), "+PONG\r\n")
	}
}

func TestProtocolErrorClosesConnection(t *testing.T) {
	c := dial(t, startServer(t))
	c.send("PING\r\n") // plain text, not a RESP array

	reply := c.readReply()
	if !strings.HasPrefix(reply, "-ERR protocol error") {
		t.Fatalf("expected a protocol error reply, got %q", reply)
	}
	if _, err := c.r.ReadString('\n'); err != io.EOF {
		t.Fatalf("expected the server to close the connection, got %v", err)
	}
}

func TestClientsShareStore(t *testing.T) {
	addr := startServer(t)
	a, b := dial(t, addr), dial(t, addr)
	expect(t, a.do("SET", "name", "alice"), "+OK\r\n")
	expect(t, b.do("GET", "name"), "$5\r\nalice\r\n")
}

func TestConcurrentClients(t *testing.T) {
	addr := startServer(t)
	for i := range 20 {
		t.Run(fmt.Sprintf("client%d", i), func(t *testing.T) {
			t.Parallel()
			c := dial(t, addr)
			key, val := fmt.Sprintf("key%d", i), fmt.Sprintf("val%d", i)
			for range 50 {
				expect(t, c.do("SET", key, val), "+OK\r\n")
				expect(t, c.do("GET", key), fmt.Sprintf("$%d\r\n%s\r\n", len(val), val))
			}
		})
	}
}
