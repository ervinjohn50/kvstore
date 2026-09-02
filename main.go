package main

import (
	"fmt"

	"github.com/ervinjohn50/kvstore/store"
)

func main() {
	s := store.New()

	s.Set("name", "alice")
	s.Set("lang", "go")

	val, ok := s.Get("name")
	fmt.Printf("GET name -> %q (found: %v)\n", val, ok)

	s.Delete("name")

	val, ok = s.Get("name")
	fmt.Printf("GET name -> %q (found: %v)\n", val, ok)

	val, ok = s.Get("lang")
	fmt.Printf("GET lang -> %q (found: %v)\n", val, ok)
}
