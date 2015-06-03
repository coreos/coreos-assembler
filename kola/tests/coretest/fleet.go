package coretest

import (
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"syscall"
	"time"
)

const (
	defaultFleetctlBinPath = "/usr/bin/fleetctl"
	defaultFleetctlTimeout = 10 * time.Second
	serviceData            = `[Unit]
Description=Hello World
[Service]
ExecStart=/bin/bash -c "while true; do echo \"Hello, world\"; sleep 1; done"
`
)

var (
	fleetctlBinPath string
	fleetctlTimeout time.Duration
)

func init() {
	fleetctlBinPath = strings.TrimSpace(os.Getenv("FLEETCTL_BIN_PATH"))
	if fleetctlBinPath == "" {
		fleetctlBinPath = defaultFleetctlBinPath
	}

	timeout := strings.TrimSpace(os.Getenv("FLEETCTL_TIMEOUT"))
	if timeout != "" {
		var err error
		fleetctlTimeout, err = time.ParseDuration(timeout)
		if err != nil {
			plog.Fatalf("Failed parsing FLEETCTL_TIMEOUT: %v", err)
		}
	} else {
		fleetctlTimeout = defaultFleetctlTimeout
	}
}

// TestFleetctlListMachines tests that 'fleetctl list-machines' works
// and print itself out at least.
func TestFleetctlListMachines() error {
	stdout, stderr, err := Run(fleetctlBinPath, "list-machines", "--no-legend")
	if err != nil {
		return fmt.Errorf("fleetctl list-machines failed with error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	stdout = strings.TrimSpace(stdout)
	if len(strings.Split(stdout, "\n")) == 0 {
		return fmt.Errorf("Failed listing out at least one machine\nstdout: %s", stdout)
	}
	return nil
}

// TestFleetctlRunService tests that fleetctl could start, unload and destroy
// unit file.
func TestFleetctlRunService() error {
	serviceName := "hello.service"

	serviceFile, err := os.Create(path.Join(os.TempDir(), serviceName))
	if err != nil {
		return fmt.Errorf("Failed creating %v: %v", serviceName, err)
	}
	defer syscall.Unlink(serviceFile.Name())

	if _, err := io.WriteString(serviceFile, serviceData); err != nil {
		return fmt.Errorf("Failed writing %v: %v", serviceFile.Name(), err)
	}

	defer timeoutFleetctl("destroy", serviceFile.Name())

	stdout, stderr, err := timeoutFleetctl("start", serviceFile.Name())
	if err != nil {
		return fmt.Errorf("fleetctl start failed with error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	stdout, stderr, err = timeoutFleetctl("unload", serviceName)
	if err != nil {
		return fmt.Errorf("fleetctl unload failed with error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	stdout, stderr, err = timeoutFleetctl("destroy", serviceName)
	if err != nil {
		return fmt.Errorf("fleetctl destroy failed with error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	return nil
}

func timeoutFleetctl(action string, unitName string) (stdout string, stderr string, err error) {
	done := make(chan struct{})

	go func() {
		stdout, stderr, err = Run(fleetctlBinPath, action, unitName)
		close(done)
	}()

	select {
	case <-time.After(fleetctlTimeout):
		return "", "", fmt.Errorf("timed out waiting for command \"%s %s\" to finish", action, unitName)
	case <-done:
		return
	}
}
