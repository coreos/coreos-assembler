package main

import (
	"fmt"
	"os"

	"github.com/marineam/gsextra/auth"
	"github.com/marineam/gsextra/index"
)

func main() {
	client, err := auth.GoogleClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Authentication failed: %v\n", err)
		os.Exit(1)
	}

	dir, err := index.NewDirectory(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if err = dir.Fetch(client); err != nil {
		fmt.Fprintf(os.Stderr, "Fetching object list failed: %v\n", err)
		os.Exit(1)
	}

	if err = dir.WriteIndex(client); err != nil {
		fmt.Fprintf(os.Stderr, "Writing directory indexs failed: %v\n", err)
		os.Exit(1)
	}
}
