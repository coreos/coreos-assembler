package main

import (
	"fmt"
	"os"

	"github.com/coreos/mantle/auth"
	"github.com/coreos/mantle/index"
)

func main() {
	client, err := auth.GoogleClient(false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Authentication failed: %v\n", err)
		os.Exit(1)
	}

	if err := index.Update(client, os.Args[1]); err != nil {
		fmt.Fprintf(os.Stderr, "Updating indexes failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Update successful!\n")
}
