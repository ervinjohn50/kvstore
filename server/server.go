// Package server accepts TCP connections and runs RESP commands against a Store.
package server

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/ervinjohn50/kvstore/resp"
	"github.com/ervinjohn50/kvstore/store"
)

// Largest EX value that still fits in a time.Duration without overflowing.
const maxTTLSeconds = math.MaxInt64 / int64(time.Second)

type Server struct {
	store *store.Store
}

// New returns a Server that runs commands against s.
func New(s *store.Store) *Server {
	return &Server{store: s}
}

// Serve accepts connections on ln until it's closed, handling each client in
// its own goroutine. The Store's mutex keeps their commands from colliding.
func (srv *Server) Serve(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go srv.handleConn(conn)
	}
}

func (srv *Server) handleConn(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)

	for {
		args, err := resp.ReadCommand(r)
		if err != nil {
			switch {
			case errors.Is(err, resp.ErrProtocol):
				// We can't find where the next command starts, so reply once and hang up.
				resp.WriteError(w, "ERR "+err.Error())
				w.Flush()
			case err != io.EOF:
				log.Printf("%s: %v", conn.RemoteAddr(), err)
			}
			return
		}

		// bufio.Writer saves the first write error and returns it from Flush,
		// so execute doesn't need to check each write.
		srv.execute(w, args)
		if err := w.Flush(); err != nil {
			return
		}
	}
}

func (srv *Server) execute(w io.Writer, args []string) {
	switch strings.ToUpper(args[0]) {
	case "PING":
		switch len(args) {
		case 1:
			resp.WriteSimpleString(w, "PONG")
		case 2:
			resp.WriteBulkString(w, args[1])
		default:
			wrongArgs(w, "ping")
		}

	case "GET":
		if len(args) != 2 {
			wrongArgs(w, "get")
			return
		}
		if val, ok := srv.store.Get(args[1]); ok {
			resp.WriteBulkString(w, val)
		} else {
			resp.WriteNull(w)
		}

	case "SET":
		srv.set(w, args)

	case "DEL":
		if len(args) < 2 {
			wrongArgs(w, "del")
			return
		}
		var deleted int64
		for _, key := range args[1:] {
			if srv.store.Delete(key) {
				deleted++
			}
		}
		resp.WriteInteger(w, deleted)

	default:
		// %q escapes "\r\n", so a crafted command name can't break the reply format.
		resp.WriteError(w, fmt.Sprintf("ERR unknown command %q", args[0]))
	}
}

// set handles SET key value [EX seconds].
func (srv *Server) set(w io.Writer, args []string) {
	if len(args) < 3 {
		wrongArgs(w, "set")
		return
	}
	key, value := args[1], args[2]

	switch {
	case len(args) == 3:
		srv.store.Set(key, value)
	case len(args) == 5 && strings.EqualFold(args[3], "EX"):
		secs, err := strconv.ParseInt(args[4], 10, 64)
		if err != nil || secs <= 0 || secs > maxTTLSeconds {
			resp.WriteError(w, "ERR invalid expire time in 'set' command")
			return
		}
		srv.store.SetWithTTL(key, value, time.Duration(secs)*time.Second)
	default:
		resp.WriteError(w, "ERR syntax error")
		return
	}
	resp.WriteSimpleString(w, "OK")
}

func wrongArgs(w io.Writer, cmd string) {
	resp.WriteError(w, fmt.Sprintf("ERR wrong number of arguments for '%s' command", cmd))
}
