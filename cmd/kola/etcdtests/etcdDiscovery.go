package etcdtests

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/coreos/mantle/platform"
)

func etcdDiscovery(cluster platform.Cluster, version int) error {
	csize := len(cluster.Machines())

	// get journalctl -f from all machines before starting
	for _, m := range cluster.Machines() {
		if err := m.StartJournal(); err != nil {
			return fmt.Errorf("failed to start journal: %v", err)
		}
	}

	// point etcd on each machine to discovery
	for i, m := range cluster.Machines() {
		// start etcd instance
		var etcdStart string
		if version == 1 {
			etcdStart = "sudo systemctl start etcd.service"
		} else if version == 2 {
			etcdStart = "sudo systemctl start etcd2.service"
		} else {
			return fmt.Errorf("etcd version unspecified")
		}

		_, err := m.SSH(etcdStart)
		if err != nil {
			return fmt.Errorf("SSH cmd to %v failed: %s", m.IP(), err)
		}
		fmt.Fprintf(os.Stderr, "etcd instance%d started\n", i)
	}

	err := getClusterHealth(cluster.Machines()[0], csize)
	if err != nil {
		return fmt.Errorf("discovery failed health check: %v", err)
	}

	fmt.Fprintf(os.Stderr, "etcd discovery succeeeded!\n")
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
		return fmt.Errorf("status unhealthy or incomplete: %s", b)
	}
}
