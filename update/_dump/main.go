package main

import (
	"fmt"
	"os"

	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/golang/protobuf/proto"

	"github.com/coreos/mantle/update"
)

func main() {
	f, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	p, err := update.NewPayloadFrom(f)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if err := proto.MarshalText(os.Stdout, &p.Manifest); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if err := p.Verify(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if err := proto.MarshalText(os.Stdout, &p.Signatures); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
