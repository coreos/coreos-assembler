package etcdtests

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/coreos/mantle/platform"
)

func etcdDiscovery(cluster platform.Cluster) error {
	csize := len(cluster.Machines())

	// get journalctl -f from only one machine
	err := cluster.Machines()[0].StartJournal()
	if err != nil {
		return fmt.Errorf("Failed to start journal: %v", err)
	}

	// point etcd on each machine to discovery
	for i, m := range cluster.Machines() {
		// start etcd instance
		etcdStart := "sudo systemctl start etcd.service"
		_, err := m.SSH(etcdStart)
		if err != nil {
			return fmt.Errorf("SSH cmd to %v failed: %s", m.IP(), err)
		}
		fmt.Fprintf(os.Stderr, "etcd instance%d started\n", i)
	}

	err = getClusterHealth(cluster.Machines()[0], csize)
	if err != nil {
		return fmt.Errorf("Discovery failed health check: %v", err)
	}

	fmt.Fprintf(os.Stderr, "etcd Discovery succeeeded!\n")
	return nil
}

// poll cluster-health until result
func getClusterHealth(m platform.Machine, csize int) error {
	const (
		retries   = 5
		retryWait = 3 * time.Second
	)
	var err error
	var b []byte

	for i := 0; i < retries; i++ {
		fmt.Fprintf(os.Stderr, "polling cluster health...\n")
		b, err = m.SSH("etcdctl cluster-health")
		if err == nil {
			break
		}
		time.Sleep(retryWait)
	}
	if err != nil {
		return fmt.Errorf("health polling failed: %s", b)
	}

	// repsonse should include "healthy" for each machine and for cluster
	if strings.Count(string(b), "healthy") == csize+1 {
		fmt.Fprintf(os.Stderr, "%s\n", b)
		return nil
	} else {
		return fmt.Errorf("Status unhealthy or incomplete: %s", b)
	}
}
