package coretest

import (
	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
)

// run various native functions that only require a single machine
func LocalTests(c cluster.TestCluster) {
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
