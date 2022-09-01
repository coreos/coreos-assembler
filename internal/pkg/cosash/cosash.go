// Package cosash implements a "co-processing" proxy that is primarily
// designed to expose a Go API that is currently implemented by `src/cmdlib.sh`.
// A lot of the code in that file is stateful - e.g. APIs set environment variables
// and allocate temporary directories.  So it wouldn't work very well to fork
// a new shell process each time.
//
// The "co-processing" here is a way to describe that there's intended to be
// a one-to-one relationship of the child bash process and the current one,
// although this is not strictly required.  The Go APIs here call dynamically
// into the bash process by writing to its stdin, and can receive serialized
// data back over a pipe on file descriptor 3.
package cosash

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"github.com/coreos/coreos-assembler/internal/pkg/bashexec"
)

// CosaSh is a companion shell process which accepts commands
// piped over stdin.
type CosaSh struct {
	cmd           *exec.Cmd
	input         io.WriteCloser
	preparedBuild bool
	ackserial     uint64
	replychan     <-chan (string)
	errchan       <-chan (error)
}

func parseAck(r *bufio.Reader, expected uint64) (string, error) {
	linebytes, _, err := r.ReadLine()
	if err != nil {
		return "", err
	}
	line := string(linebytes)
	parts := strings.SplitN(line, " ", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid reply from cosash: %s", line)
	}
	serial, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid reply from cosash: %s", line)
	}
	if serial != expected {
		return "", fmt.Errorf("unexpected ack serial from cosash; expected=%d reply=%d", expected, serial)
	}
	return parts[1], nil
}

// NewCosaSh creates a new companion shell process
func NewCosaSh() (*CosaSh, error) {
	cmd := exec.Command("/bin/bash")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGTERM,
	}
	// This is the channel where we send our commands
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	// stdout and stderr are the same as ours; we are effectively
	// "co-processing", so we want to get output/errors as they're
	// printed.
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	cmdin, cmdout, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd.ExtraFiles = append(cmd.ExtraFiles, cmdout)

	// Start the process
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	replychan := make(chan string)
	errchan := make(chan error)

	r := &CosaSh{
		input:         input,
		cmd:           cmd,
		replychan:     replychan,
		errchan:       errchan,
		preparedBuild: false,
	}

	// Send a message when the process exits
	go func() {
		errchan <- cmd.Wait()
	}()
	// Parse the ack serials into a channel
	go func() {
		bufr := bufio.NewReader(cmdin)
		for {
			reply, err := parseAck(bufr, r.ackserial)
			if err != nil {
				// Don't propagate EOF, since we want the process exit status instead.
				if err == io.EOF {
					break
				}
				errchan <- err
				break
			}
			r.ackserial += 1
			replychan <- reply
		}
	}()

	// Initialize the internal library
	err = r.Process(fmt.Sprintf("%s\n. /usr/lib/coreos-assembler/cmdlib.sh\n", bashexec.StrictMode))
	if err != nil {
		return nil, fmt.Errorf("failed to init cosash: %w", err)
	}

	return r, nil
}

// write sends content to the shell's stdin, synchronously wait for the reply
func (r *CosaSh) ProcessWithReply(buf string) (string, error) {
	// Inject code which writes the serial reply prefix
	cmd := fmt.Sprintf("echo -n \"%d \" >&3\n", r.ackserial)
	if _, err := io.WriteString(r.input, cmd); err != nil {
		return "", err
	}
	// Tell the shell to execute the code, which should write the reply to fd 3
	// which will complete the command.
	if _, err := io.WriteString(r.input, buf); err != nil {
		return "", err
	}

	select {
	case reply := <-r.replychan:
		return reply, nil
	case err := <-r.errchan:
		return "", err
	}
}

func (sh *CosaSh) Process(buf string) error {
	buf = fmt.Sprintf("%s\necho OK >&3\n", buf)
	r, err := sh.ProcessWithReply(buf)
	if err != nil {
		return err
	}
	if r != "OK" {
		return fmt.Errorf("unexpected reply from cosash; expected OK, found %s", r)
	}
	return nil
}

// PrepareBuild prepares for a build, returning the newly allocated build directory
func (sh *CosaSh) PrepareBuild() (string, error) {
	return sh.ProcessWithReply(`prepare_build
pwd >&3
`)
}

// HasPrivileges checks if we can use sudo
func (sh *CosaSh) HasPrivileges() (bool, error) {
	r, err := sh.ProcessWithReply(`
if has_privileges; then
  echo true >&3
else
  echo false >&3
fi`)
	if err != nil {
		return false, err
	}
	return strconv.ParseBool(r)
}
