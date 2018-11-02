// Copyright 2015 CoreOS, Inc.
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

package misc

import (
	"fmt"
	"time"

	"github.com/coreos/go-omaha/omaha"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform/conf"
	"github.com/coreos/mantle/platform/machine/qemu"
)

func init() {
	register.Register(&register.Test{
		Run:         OmahaPing,
		ClusterSize: 0,
		Name:        "cl.omaha.ping",
		Platforms:   []string{"qemu"},
		Distros:     []string{"cl"},
	})
}

type pingServer struct {
	omaha.UpdaterStub

	ping chan struct{}
}

func (ps *pingServer) Ping(req *omaha.Request, app *omaha.AppRequest) {
	ps.ping <- struct{}{}
}

func OmahaPing(c cluster.TestCluster) {
	qc, ok := c.Cluster.(*qemu.Cluster)
	if !ok {
		c.Fatal("test only works in qemu")
	}

	omahaserver := qc.LocalCluster.OmahaServer

	svc := &pingServer{
		ping: make(chan struct{}),
	}

	omahaserver.Updater = svc

	hostport, err := qc.GetOmahaHostPort()
	if err != nil {
		c.Fatalf("couldn't get Omaha server address: %v", err)
	}
	config := fmt.Sprintf(`update:
  server: "http://%s/v1/update/"
`, hostport)

	m, err := c.NewMachine(conf.ContainerLinuxConfig(config))
	if err != nil {
		c.Fatalf("couldn't start machine: %v", err)
	}

	out, stderr, err := m.SSH("update_engine_client -check_for_update")
	if err != nil {
		c.Fatalf("couldn't check for update: %s, %s, %v", out, stderr, err)
	}

	tc := time.After(30 * time.Second)

	select {
	case <-tc:
		c.Fatal("timed out waiting for omaha ping")
	case <-svc.ping:
	}
}
