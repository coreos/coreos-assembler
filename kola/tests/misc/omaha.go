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
	"errors"
	"fmt"
	"time"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/network/omaha"
	"github.com/coreos/mantle/platform"
)

func init() {
	register.Register(&register.Test{
		Run:         OmahaPing,
		ClusterSize: 1,
		Name:        "coreos.omaha.ping",
		Platforms:   []string{"qemu"},
		UserData: `#cloud-config

coreos:
  update:
    server: "http://10.0.0.1:34567/v1/update/"
`,
	})
}

type pingServer struct {
	omaha.UpdaterStub

	ping chan struct{}
}

func (ps *pingServer) Ping(req *omaha.Request, app *omaha.AppRequest) {
	ps.ping <- struct{}{}
}

func OmahaPing(c cluster.TestCluster) error {
	qc, ok := c.Cluster.(*platform.QEMUCluster)
	if !ok {
		return errors.New("test only works in qemu")
	}

	omahaserver := qc.LocalCluster.OmahaServer

	svc := &pingServer{
		ping: make(chan struct{}),
	}

	omahaserver.Updater = svc

	m := c.Machines()[0]

	out, err := m.SSH("update_engine_client -check_for_update")
	if err != nil {
		return fmt.Errorf("failed to execute update_engine_client -check_for_update: %v: %v", out, err)
	}

	tc := time.After(30 * time.Second)

	select {
	case <-tc:
		platform.Manhole(m)
		return errors.New("timed out waiting for omaha ping")
	case <-svc.ping:
	}

	return nil
}
