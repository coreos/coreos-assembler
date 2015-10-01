package coretest

import (
	"fmt"
	"time"

	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/coreos/pkg/capnslog"
	"github.com/coreos/mantle/kola/tests/etcd"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/util"
)

var plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "kola/tests/coretest")

// run various native functions that only require a single machine
func LocalTests(c platform.TestCluster) error {
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
func ClusterTests(c platform.TestCluster) error {
	if plog.LevelAt(capnslog.DEBUG) {
		// get journalctl -f from all machines before starting
		for _, m := range c.Machines() {
			if err := m.StartJournal(); err != nil {
				return fmt.Errorf("failed to start journal: %v", err)
			}
		}
	}

	// make sure etcd is up and running
	var keyMap map[string]string
	var retryFuncs []func() error

	retryFuncs = append(retryFuncs, func() error {
		var err error
		keyMap, err = etcd.SetKeys(c, 3)
		if err != nil {
			return err
		}
		return nil
	})
	retryFuncs = append(retryFuncs, func() error {
		if err := etcd.CheckKeys(c, keyMap, true); err != nil {
			return err
		}
		return nil
	})
	for _, retry := range retryFuncs {
		if err := util.Retry(5, 5*time.Second, retry); err != nil {
			return fmt.Errorf("etcd failed health check: %v", err)
		}
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
func InternetTests(c platform.TestCluster) error {
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
