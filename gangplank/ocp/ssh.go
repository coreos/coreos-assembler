package ocp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/coreos/gangplank/spec"
	log "github.com/sirupsen/logrus"
)

type SSHForwardPort struct {
	Host string
	User string

	// port is not exported
	port int
}

// getSshMinioForwarder returns an SSHForwardPort from the jobspec
// definition for forwarding a minio server, or nil if forwarding is
// not enabled.
func getSshMinioForwarder(j *spec.JobSpec) *SSHForwardPort {
	if j.Job.MinioSSHForward == "" {
		return nil
	}
	return &SSHForwardPort{
		Host: j.Job.MinioSSHForward,
		User: j.Job.MinioSSHUser,
	}
}

// sshForwarder is a generic forwarder from the local host to a remote host
func sshForwarder(ctx context.Context, cfg *SSHForwardPort) (chan<- bool, error) {
	termCh := make(chan bool, 2048)

	if cfg == nil {
		return nil, errors.New("configuration for SSH forwarding is nil")
	}

	var cmd *exec.Cmd
	run := func() error {
		host := cfg.Host
		if cfg.User != "" && !strings.Contains(cfg.Host, "@") {
			host = fmt.Sprintf("%s@%s", cfg.User, cfg.Host)
		}
		args := []string{"ssh", "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=5", "-N", "-R",
			fmt.Sprintf("%d:127.0.0.1:%d", cfg.port, cfg.port), host,
		}

		cmd = exec.CommandContext(ctx, args[0], args[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			return err
		}
		// wait for SSH to start
		time.Sleep(5 * time.Second)
		return nil
	}

	// Give a first start to make sure it works
	if err := run(); err != nil {
		return nil, err
	}

	// Run and monitor ssh
	go func() {

		// term kills the SSH forwarding command, and then unsets
		term := func() {
			log.Info("terminating ssh forwarder")
			_ = syscall.Kill(cmd.Process.Pid, syscall.SIGTERM)
			cmd = nil
		}

		// monitor ensures that the ssh command is running
		monitor := func() <-chan bool {
			done := make(chan bool)
			go func() {
				for {
					select {
					// terminate the monitor function if singalled
					case t, ok := <-termCh:
						if t || !ok {
							return
						}
					// wait until the command terminates
					default:
						// do nothing if cmd has been undefined
						if cmd == nil {
							continue
						}
						// wait until cmd completes
						_ = cmd.Wait()
						done <- true
					}
				}
			}()
			return done
		}

		failCount := 0
		for {
			select {
			// terminate the ssh command and exit on signal
			case t, ok := <-termCh:
				if t || !ok {
					term()
					return
				}
			// in case of exit from ssh, restart up to three times and log the message
			case <-monitor():
				log.Infof("ssh forwarder exited")
				if failCount <= 3 {
					log.Warn("restarting ssh")
					_ = run()
					failCount++
					continue
				}
				log.Errorf("ssh forwarder exited with code %d", cmd.ProcessState.ExitCode())
				return
			}
		}
	}()

	return termCh, nil
}
