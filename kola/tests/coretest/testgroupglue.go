package coretest

import (
	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/tests/etcd"
)

// run various native functions that only require a single machine
func LocalTests(c cluster.TestCluster) {
	tests := c.ListNativeFunctions()
	for _, name := range tests {
		c.RunNative(name, c.Machines()[0])
	}
}

// run clustering based tests
func ClusterTests(c cluster.TestCluster) {
	// wait for etcd to come up
	if err := etcd.GetClusterHealth(c.Machines()[0], len(c.Machines())); err != nil {
		c.Fatal(err)
	}

	tests := c.ListNativeFunctions()
	for _, name := range tests {
		c.RunNative(name, c.Machines()[0])
	}

}

// run internet based tests
func InternetTests(c cluster.TestCluster) {
	tests := c.ListNativeFunctions()
	for _, name := range tests {
		c.RunNative(name, c.Machines()[0])
	}
}
