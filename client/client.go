// Package client sends commands to a kvstore server over a single TCP connection.
package client

import (
	"bufio"
	"net"

	"github.com/ervinjohn50/kvstore/resp"
)

// Client is one connection to a server. It isn't safe for concurrent use,
// since replies arrive in order and would get mixed up; open one per goroutine.
type Client struct {
	conn net.Conn
	r    *bufio.Reader
	w    *bufio.Writer
}

// Dial connects to the server at addr, like "127.0.0.1:6380".
func Dial(addr string) (*Client, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &Client{conn: conn, r: bufio.NewReader(conn), w: bufio.NewWriter(conn)}, nil
}

// Do sends one command and waits for its reply. Server-side errors like
// "ERR syntax error" come back as a Reply of Kind resp.Error, not as err.
func (c *Client) Do(args ...string) (resp.Reply, error) {
	if err := resp.WriteCommand(c.w, args); err != nil {
		return resp.Reply{}, err
	}
	if err := c.w.Flush(); err != nil {
		return resp.Reply{}, err
	}
	return resp.ReadReply(c.r)
}

// Close closes the connection.
func (c *Client) Close() error {
	return c.conn.Close()
}
