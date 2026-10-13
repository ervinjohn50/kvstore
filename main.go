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
	flag.Parse()

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("kvstore listening on %s", ln.Addr())
	log.Fatal(server.New(store.New()).Serve(ln))
}
