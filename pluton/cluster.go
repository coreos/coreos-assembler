// Copyright 2017 CoreOS, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pluton

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/coreos/mantle/harness"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/util"
)

// Cluster represents an interface to test kubernetes clusters. The harness
// object is used for logging, exiting or skipping a test. Is nearly
// identical to the Go test harness. The creation is usually implemented by a
// function that builds the Cluster from a kola TestCluster from the 'spawn'
// subpackage. Tests may be aware of the implementor function since not all
// clusters are expected to have the same components nor properties.
type Cluster struct {
	Masters []platform.Machine
	Workers []platform.Machine
	Info    Info

	*harness.H

	m Manager
}

// Info contains information about how a Cluster is configured that may be
// useful for some tests.
type Info struct {
	KubeletTag      string // e.g. v1.5.3_coreos.0
	Version         string // e.g. v1.5.3+coreos.0
	UpstreamVersion string // e.g. v1.5.3
}

func NewCluster(m Manager, masters, workers []platform.Machine, info Info) *Cluster {
	return &Cluster{
		Masters: masters,
		Workers: workers,
		Info:    info,
		m:       m,
	}
}

// Kubectl will run kubectl from /home/core on the Master Machine
func (c *Cluster) Kubectl(cmd string) (string, error) {
	stdout, stderr, err := c.SSH("sudo ./kubectl --kubeconfig=/etc/kubernetes/kubeconfig " + cmd)
	if err != nil {
		return "", fmt.Errorf("kubectl: %s", stderr)
	}
	return string(stdout), nil
}

// Ready blocks until a Cluster is considered available. The current
// implementation only checks that all nodes are Registered. TODO: Use the
// manager interface to allow the implementor to determine when a Cluster is
// considered available either by exposing enough information for this function
// to check that certain pods are running or implementing its own `Ready()
// error` function that gets called to after the nodeCheck in this function.
func (c *Cluster) Ready() error {
	return nodeCheck(c, 50)
}

// AddMasters creates new master nodes for a Cluster and blocks until ready.
func (c *Cluster) AddMasters(n int) error {
	nodes, err := c.m.AddMasters(n)
	if err != nil {
		return err
	}

	c.Masters = append(c.Masters, nodes...)

	if err := c.Ready(); err != nil {
		return err
	}
	return nil
}

// SSH is just a convenience function for running SSH commands when you don't
// care which machine the command runs on. The current implementation chooses
// the first master node. The signature is slightly different then the machine
// SSH command and doesn't automatically print stderr. I expect in the future
// that this will be more unified with the Machine.SSH signature, but for now
// this is useful to silence all the retry loops from clogging up the test
// results while giving the option to deal with stderr.
func (c *Cluster) SSH(cmd string) (stdout, stderr []byte, err error) {
	client, err := c.Masters[0].SSHClient()
	if err != nil {
		return nil, nil, err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return nil, nil, err
	}
	defer session.Close()

	outBuf := bytes.NewBuffer(nil)
	errBuf := bytes.NewBuffer(nil)
	session.Stdout = outBuf
	session.Stderr = errBuf

	err = session.Run(cmd)

	stdout = bytes.TrimSpace(outBuf.Bytes())
	stderr = bytes.TrimSpace(errBuf.Bytes())

	return stdout, stderr, err
}

// nodeCheck will parse kubectl output to ensure all nodes in Cluster are
// registered. Set retry for max amount of retries to attempt before erroring.
func nodeCheck(c *Cluster, retryAttempts int) error {
	f := func() error {
		out, err := c.Kubectl("get nodes")
		if err != nil {
			return err
		}

		// parse kubectl output for IPs
		addrMap := map[string]struct{}{}
		for _, line := range strings.Split(out, "\n")[1:] {
			addrMap[strings.SplitN(line, " ", 2)[0]] = struct{}{}
		}

		nodes := append(c.Workers, c.Masters...)

		if len(addrMap) != len(nodes) {
			return fmt.Errorf("cannot detect all nodes in kubectl output \n%v\n%v", addrMap, nodes)
		}
		for _, node := range nodes {
			if _, ok := addrMap[node.PrivateIP()]; !ok {
				return fmt.Errorf("node IP missing from kubectl get nodes")
			}
		}
		return nil
	}

	if err := util.Retry(retryAttempts, 10*time.Second, f); err != nil {
		return err
	}
	return nil
}
