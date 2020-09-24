package main

import (
	"fmt"
	"os"

	"github.com/coreos/mantle/update"
)

func main() {
	u := update.Updater{
		DstPartition: "out",
	}

	if err := u.OpenPayload(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if err := u.Update(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
