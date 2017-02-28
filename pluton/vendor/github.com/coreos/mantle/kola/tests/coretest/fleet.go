package coretest

import (
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"syscall"
	"time"

	"github.com/coreos/mantle/util"
)

const (
	serviceData = `[Unit]
Description=Hello World
[Service]
ExecStart=/bin/bash -c "while true; do echo \"Hello, world\"; sleep 1; done"
`
)

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

	myid := MachineID()

	fleetChecker := func() error {
		stdout, stderr, err := Run("fleetctl", "list-machines", "-no-legend", "-l", "-fields", "machine")
		if err != nil {
			return fmt.Errorf("fleetctl list-machines failed with error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}

		if !strings.Contains(stdout, myid) {
			return fmt.Errorf("fleetctl list-machines: machine ID %q missing from output\nstdout: %v\nstderr: %v", myid, stdout, stderr)
		}

		return nil
	}

	if err := util.Retry(5, 5*time.Second, fleetChecker); err != nil {
		return err
	}

	stdout, stderr, err := Run("fleetctl", "start", "-block-attempts", "20", serviceFile.Name())
	if err != nil {
		return fmt.Errorf("fleetctl start failed with error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	stdout, stderr, err = Run("fleetctl", "unload", "-block-attempts", "20", serviceName)
	if err != nil {
		return fmt.Errorf("fleetctl unload failed with error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	stdout, stderr, err = Run("fleetctl", "destroy", serviceName)
	if err != nil {
		return fmt.Errorf("fleetctl destroy failed with error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	return nil
}
