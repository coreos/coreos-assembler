package coretest

import (
	"io"
	"os"
	"path"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	fleetctlBinPath = "/usr/bin/fleetctl"
	tryTimes = 5
	tryInterval = 500 * time.Millisecond
	serviceData = `[Unit]
Description=Hello World
[Service]
ExecStart=/bin/bash -c "while true; do echo \"Hello, world\"; sleep 1; done"
`
)

// TestFleetctlListMachines tests that 'fleetctl list-machines' works
// and print itself out at least.
func TestFleetctlListMachines(t *testing.T) {
	stdout, stderr, err := Run(fleetctlBinPath, "list-machines", "--no-legend")
	if err != nil {
		t.Fatalf("fleetctl list-machines failed with error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	stdout = strings.TrimSpace(stdout)
	if len(strings.Split(stdout, "\n")) == 0 {
		t.Fatalf("Failed listing out at least one machine\nstdout: %s", stdout)
	}
}

func checkServiceState(name string, t *testing.T) (exist bool, active bool) {
	stdout, stderr, err := Run(fleetctlBinPath, "list-units", "--no-legend")
	if err != nil {
		t.Fatalf("fleetctl list-units failed with error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	lines := strings.Split(stdout, "\n")
	for _, line := range lines {
		if !strings.Contains(line, name) {
			continue
		}
		exist = true
		if strings.Contains(line, "running") {
			active = true
			return
		}
		return
	}
	return
}

// TestFleetctlRunService tests that fleetctl could start, stop and destroy
// unit file.
func TestFleetctlRunService(t *testing.T) {
	serviceName := "hello.service"

	serviceFile, err := os.Create(path.Join(os.TempDir(), serviceName))
	if err != nil {
		t.Fatalf("Failed creating %v: %v", serviceName, err)
	}
	defer syscall.Unlink(serviceFile.Name())

	if _, err := io.WriteString(serviceFile, serviceData); err != nil {
		t.Fatalf("Failed writing %v: %v", serviceFile.Name(), err)
	}

	stdout, stderr, err := Run(fleetctlBinPath, "start", serviceFile.Name())
	if err != nil {
		t.Fatalf("fleetctl start failed with error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	checkServiceActive := func() bool {
		exist, active := checkServiceState(serviceName, t)
		return exist && active
	}
	if !Retry(checkServiceActive, tryTimes, tryInterval) {
		t.Fatalf("Failed checking %v is active", serviceName)
	}

	stdout, stderr, err = Run(fleetctlBinPath, "stop", serviceName)
	if err != nil {
		t.Fatalf("fleetctl stop failed with error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	checkServiceInactive := func() bool {
		exist, active := checkServiceState(serviceName, t)
		return exist && !active
	}
	if !Retry(checkServiceInactive, tryTimes, tryInterval) {
		t.Fatalf("Failed checking %v is inactive", serviceName)
	}

	stdout, stderr, err = Run(fleetctlBinPath, "destroy", serviceName)
	if err != nil {
		t.Fatalf("fleetctl destroy failed with error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}

	checkServiceNonexist := func() bool {
		exist, _ := checkServiceState(serviceName, t)
		return !exist
	}
	if !Retry(checkServiceNonexist, tryTimes, tryInterval) {
		t.Fatalf("Failed checking %v is nonexist", serviceName)
	}
}
