package main

import (
	"bytes"
	"net"
	"slices"
	"strings"
	"testing"

	"github.com/ervinjohn50/kvstore/client"
	"github.com/ervinjohn50/kvstore/resp"
	"github.com/ervinjohn50/kvstore/server"
	"github.com/ervinjohn50/kvstore/store"
)

func TestSplitArgs(t *testing.T) {
	tests := []struct {
		line string
		want []string
	}{
		{"SET name alice", []string{"SET", "name", "alice"}},
		{"  GET   name  ", []string{"GET", "name"}},
		{"", nil},
		{`SET greeting "hello world"`, []string{"SET", "greeting", "hello world"}},
		{`SET greeting 'hello world'`, []string{"SET", "greeting", "hello world"}},
		{`SET k ""`, []string{"SET", "k", ""}},
		{`SET k "say \"hi\""`, []string{"SET", "k", `say "hi"`}},
		{`SET k 'back\slash'`, []string{"SET", "k", `back\slash`}},
		{`SET k ab"c d"`, []string{"SET", "k", "abc d"}},
	}
	for _, tt := range tests {
		got, err := splitArgs(tt.line)
		if err != nil {
			t.Fatalf("splitArgs(%q): unexpected error: %v", tt.line, err)
		}
		if !slices.Equal(got, tt.want) {
			t.Fatalf("splitArgs(%q) = %q, want %q", tt.line, got, tt.want)
		}
	}
}

func TestSplitArgsUnbalancedQuotes(t *testing.T) {
	for _, line := range []string{`SET k "abc`, `SET k 'abc`, `SET k "abc\"`} {
		if _, err := splitArgs(line); err == nil {
			t.Fatalf("splitArgs(%q): expected an error", line)
		}
	}
}

func TestFormat(t *testing.T) {
	tests := []struct {
		reply resp.Reply
		want  string
	}{
		{resp.Reply{Kind: resp.SimpleString, Value: "OK"}, "OK"},
		{resp.Reply{Kind: resp.Error, Value: "ERR boom"}, "(error) ERR boom"},
		{resp.Reply{Kind: resp.Integer, Value: "2"}, "(integer) 2"},
		{resp.Reply{Kind: resp.BulkString, Value: "hello world"}, `"hello world"`},
		{resp.Reply{Kind: resp.BulkString, Value: "a\r\nb"}, `"a\r\nb"`},
		{resp.Reply{Kind: resp.BulkString, Null: true}, "(nil)"},
	}
	for _, tt := range tests {
		if got := format(tt.reply); got != tt.want {
			t.Fatalf("format(%+v) = %q, want %q", tt.reply, got, tt.want)
		}
	}
}

func TestREPL(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go server.New(store.New(0)).Serve(ln)

	c, err := client.Dial(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })

	in := strings.NewReader(strings.Join([]string{
		"PING",
		`set greeting "hello world"`,
		"GET greeting",
		"",
		"GET missing",
		"DEL greeting",
		"FOO",
		`GET "unclosed`,
		"quit",
		"PING", // never sent, since quit ends the session
	}, "\n"))
	var out bytes.Buffer
	if err := repl(c, "> ", in, &out); err != nil {
		t.Fatal(err)
	}

	want := strings.Join([]string{
		"> PONG",
		"> OK",
		`> "hello world"`,
		"> > (nil)", // the empty line just shows the prompt again
		"> (integer) 1",
		`> (error) ERR unknown command "FOO"`,
		"> (error) unbalanced quotes",
		"> ",
	}, "\n")
	if out.String() != want {
		t.Fatalf("got:\n%s\n\nwant:\n%s", out.String(), want)
	}
}
