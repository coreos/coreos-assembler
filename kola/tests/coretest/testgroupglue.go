package coretest

import (
	"fmt"

	"github.com/coreos/mantle/platform"
)

// run various native functions that only require a single machine
func LocalTests(c platform.TestCluster) error {
	tests := []string{
		"CloudConfig",
		"Script",
		"PortSSH",
		"DbusPerms",
		"Symlink",
		"UpdateEngineKeys",
		"ServicesActive",
		"ReadOnly",
	}
	for _, name := range tests {
		fmt.Printf("running %v...\n", name)
		err := c.RunNative(name, c.Machines()[0])
		if err != nil {
			return err
		}
	}
	return nil
}

// run clustering based tests
func ClusterTests(c platform.TestCluster) error {
	tests := []string{
		"EtcdUpdateValue",
		"FleetctlListMachines",
		"FleetctlRunService",
	}
	for _, name := range tests {
		fmt.Printf("running %v...\n", name)
		err := c.RunNative(name, c.Machines()[0])
		if err != nil {
			return err
		}
	}
	return nil

}

// run internet based tests
func InternetTests(c platform.TestCluster) error {
	tests := []string{
		"UpdateEngine",
		"DockerPing",
		"DockerEcho",
		"NTPDate",
	}
	for _, name := range tests {
		fmt.Printf("running %v...\n", name)
		err := c.RunNative(name, c.Machines()[0])
		if err != nil {
			return err
		}
	}
	return nil
}
