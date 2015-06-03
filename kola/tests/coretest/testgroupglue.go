package coretest

import (
	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/coreos/pkg/capnslog"
	"github.com/coreos/mantle/platform"
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
