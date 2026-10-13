// Command kvcli is a command-line client for kvstore, similar to redis-cli.
//
//	kvcli                  start an interactive prompt
//	kvcli SET name alice   run one command and exit
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode"

	"github.com/ervinjohn50/kvstore/client"
	"github.com/ervinjohn50/kvstore/resp"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:6380", "server address")
	flag.Parse()

	c, err := client.Dial(*addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not connect to %s: %v\n", *addr, err)
		os.Exit(1)
	}
	defer c.Close()

	// The shell has already split and unquoted the arguments here.
	if flag.NArg() > 0 {
		reply, err := c.Do(flag.Args()...)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(format(reply))
		return
	}

	if err := repl(c, *addr+"> ", os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// repl reads commands line by line until "quit", "exit", or end of input (Ctrl+D).
func repl(c *client.Client, prompt string, in io.Reader, out io.Writer) error {
	sc := bufio.NewScanner(in)
	for {
		fmt.Fprint(out, prompt)
		if !sc.Scan() {
			fmt.Fprintln(out)
			return sc.Err()
		}

		args, err := splitArgs(sc.Text())
		if err != nil {
			fmt.Fprintf(out, "(error) %v\n", err)
			continue
		}
		if len(args) == 0 {
			continue
		}
		if strings.EqualFold(args[0], "quit") || strings.EqualFold(args[0], "exit") {
			return nil
		}

		reply, err := c.Do(args...)
		if err != nil {
			return fmt.Errorf("connection lost: %w", err)
		}
		fmt.Fprintln(out, format(reply))
	}
}

// splitArgs splits a line on spaces, except inside "double" or 'single'
// quotes, so `SET greeting "hello world"` is three arguments. Inside double
// quotes, a backslash makes the next character literal: "say \"hi\"".
func splitArgs(line string) ([]string, error) {
	var args []string
	var cur strings.Builder
	inArg := false // tracks "" separately from no argument at all
	var quote rune // the open quote character, or 0 if none
	escaped := false

	for _, ch := range line {
		switch {
		case escaped:
			cur.WriteRune(ch)
			escaped = false
		case quote == '"' && ch == '\\':
			escaped = true
		case quote != 0 && ch == quote:
			quote = 0
		case quote != 0:
			cur.WriteRune(ch)
		case ch == '"' || ch == '\'':
			quote = ch
			inArg = true
		case unicode.IsSpace(ch):
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteRune(ch)
			inArg = true
		}
	}
	if quote != 0 {
		return nil, errors.New("unbalanced quotes")
	}
	if inArg {
		args = append(args, cur.String())
	}
	return args, nil
}

// format prints a reply the way redis-cli does.
func format(r resp.Reply) string {
	switch {
	case r.Null:
		return "(nil)"
	case r.Kind == resp.Error:
		return "(error) " + r.Value
	case r.Kind == resp.Integer:
		return "(integer) " + r.Value
	case r.Kind == resp.BulkString:
		// Quoting shows exactly what's stored, including spaces and "\r\n".
		return strconv.Quote(r.Value)
	default:
		return r.Value
	}
}
