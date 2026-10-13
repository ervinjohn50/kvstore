package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/ervinjohn50/kvstore/server"
	"github.com/ervinjohn50/kvstore/store"
)

func main() {
	// Only accept connections from this machine by default, since there's no authentication.
	addr := flag.String("addr", "127.0.0.1:6380", "address to listen on")
	maxKeys := flag.Int("maxkeys", 0, "evict least recently used keys beyond this many (0 means no limit)")
	aofPath := flag.String("aof", "", "append-only file to persist data in (empty means memory only)")
	flag.Parse()

	if *maxKeys < 0 {
		log.Fatal("-maxkeys can't be negative")
	}

	var s *store.Store
	if *aofPath == "" {
		s = store.New(*maxKeys)
	} else {
		var err error
		if s, err = store.Open(*aofPath, *maxKeys); err != nil {
			log.Fatal(err)
		}
	}

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("kvstore listening on %s (maxkeys=%d, aof=%q)", ln.Addr(), *maxKeys, *aofPath)

	// On Ctrl+C or a termination signal, stop accepting clients so Serve
	// returns, then sync the append-only file before exiting.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	if err := server.New(s).Serve(ln); !errors.Is(err, net.ErrClosed) {
		log.Fatal(err)
	}
	log.Print("shutting down")
	if err := s.Close(); err != nil {
		log.Fatal(err)
	}
}
