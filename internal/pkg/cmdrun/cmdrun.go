package cmdrun

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// Synchronously invoke a command, logging the command arguments
// to stdout.
func RunCmdSyncV(cmdName string, args ...string) error {
	fmt.Printf("Running: %s %s\n", cmdName, strings.Join(args, " "))
	return RunCmdSync(cmdName, args...)
}

// Synchronously invoke a command, passing both stdout and stderr.
func RunCmdSync(cmdName string, args ...string) error {
	cmd := exec.Command(cmdName, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGTERM,
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("error running %s %s: %w", cmdName, strings.Join(args, " "), err)
	}

	return nil
}
