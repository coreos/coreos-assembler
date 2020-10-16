package exec

import (
	"context"
	"errors"
	"os/exec"
	"sync"
)

// RunCmds runs args as a command and ensures that each
func RunCmds(cmd *exec.Cmd) (int, error) {
	if cmd == nil {
		return 1, errors.New("No command to execute")
	}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go RemoveZombies(ctx, &wg)

	var rc int
	err := Run(cmd)
	if err != nil {
		rc = 1
	}

	cancel()
	wg.Wait()
	return rc, err
}
