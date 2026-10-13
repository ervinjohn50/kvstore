package main

import (
	"flag"
	"log"
	"net"

	"github.com/ervinjohn50/kvstore/server"
	"github.com/ervinjohn50/kvstore/store"
)

func main() {
	// Only accept connections from this machine by default, since there's no authentication.
	addr := flag.String("addr", "127.0.0.1:6380", "address to listen on")
	maxKeys := flag.Int("maxkeys", 0, "evict least recently used keys beyond this many (0 means no limit)")
	flag.Parse()

	if *maxKeys < 0 {
		log.Fatal("-maxkeys can't be negative")
	}

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("kvstore listening on %s (maxkeys=%d)", ln.Addr(), *maxKeys)
	log.Fatal(server.New(store.New(*maxKeys)).Serve(ln))
}
