package coretest

import (
	"fmt"

	"github.com/coreos/pkg/capnslog"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/tests/etcd"
	"github.com/coreos/mantle/platform"
)

var plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "kola/tests/coretest")

// run various native functions that only require a single machine
func LocalTests(c cluster.TestCluster) error {
	tests := c.ListNativeFunctions()
	for _, name := range tests {
		plog.Noticef("running %v...", name)
		err := c.RunNative(name, c.Machines()[0])
		if err != nil {
			return err
		}
	}
	return nil
}

// run clustering based tests
func ClusterTests(c cluster.TestCluster) error {
	if plog.LevelAt(capnslog.DEBUG) {
		// get journalctl -f from all machines before starting
		for _, m := range c.Machines() {
			if err := platform.StreamJournal(m); err != nil {
				return fmt.Errorf("failed to start journal: %v", err)
			}
		}
	}

	// wait for etcd to come up
	if err := etcd.GetClusterHealth(c.Machines()[0], len(c.Machines())); err != nil {
		return err
	}

	tests := c.ListNativeFunctions()
	for _, name := range tests {
		plog.Noticef("running %v...", name)
		err := c.RunNative(name, c.Machines()[0])
		if err != nil {
			return err
		}
	}
	return nil

}

// run internet based tests
func InternetTests(c cluster.TestCluster) error {
	tests := c.ListNativeFunctions()
	for _, name := range tests {
		plog.Noticef("running %v...", name)
		err := c.RunNative(name, c.Machines()[0])
		if err != nil {
			return err
		}
	}
	return nil
}
