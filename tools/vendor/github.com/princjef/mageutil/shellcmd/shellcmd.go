package shellcmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/fatih/color"
)

// Command defines a command which can be defined and run with output piped to
// stdout/stderr.
type Command string

// Run executes the command, piping its output to stdout/stderr and reporting
// any errors surfaced by it.
func (c Command) Run() error {
	args, err := new(cmdParser).parse(string(c))
	if err != nil {
		return err
	}

	fmt.Printf("%s %s\n", color.MagentaString(">"), color.New(color.Bold).Sprintf(string(c)))
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Output executes the command, capturing its stdout and stderr into a []byte,
// which is returned when the command completes.
func (c Command) Output() ([]byte, error) {
	args, err := new(cmdParser).parse(string(c))
	if err != nil {
		return nil, err
	}

	return exec.Command(args[0], args[1:]...).Output()
}

// RunAll executes all of the provided commands in sequence, only executing the
// next command if the previous command succeeded. If any of the commands fail,
// the rest are not executed and the error is returned.
func RunAll(commands ...Command) error {
	for _, cmd := range commands {
		if err := cmd.Run(); err != nil {
			return err
		}
	}

	return nil
}
