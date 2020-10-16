package exec

import (
	"context"
	"os"
	"os/exec"
	"sync"
)

// RunCmds runs args as a command and ensures that each
func RunCmds(args []string) (int, error) {
	if len(args) <= 1 {
		os.Exit(0)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go RemoveZombies(ctx, &wg)

	var rc int
	cmd := exec.Command(args[0], args[1:]...)
	err := Run(cmd)
	if err != nil {
		rc = 1
	}

	cancel()
	wg.Wait()
	return rc, err
}
