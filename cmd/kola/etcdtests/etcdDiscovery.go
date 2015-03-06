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

	fmt.Fprintf(os.Stderr, "Waiting for discovery to finish...\n")
	time.Sleep(1 * time.Second)

	// get status of etcd instances
	cmd := cluster.NewCommand("curl", "-L", "http://10.0.0.2:4001/v2/machines")
	b, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("Error requesting cluster members: %v", err)
	}
	members := strings.Split(string(b), ",")
	if len(members) != csize {
		return fmt.Errorf("Etcd members doesn't match cluster size: %v", len(members))
	}
	fmt.Fprintf(os.Stderr, "Etcd running with the following members:\n")
	for _, m := range members {
		fmt.Fprintf(os.Stderr, "	%v\n", m)
	}

	return nil
}
