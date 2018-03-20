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

package oci

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"

	"github.com/coreos/pkg/capnslog"

	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/api/oci"
	"github.com/coreos/mantle/platform/conf"
)

const (
	Platform platform.Name = "oci"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "platform/machine/oci")
)

type cluster struct {
	*platform.BaseCluster
	api    *oci.API
	sshKey string
}

func NewCluster(opts *oci.Options, rconf *platform.RuntimeConfig) (platform.Cluster, error) {
	api, err := oci.New(opts)
	if err != nil {
		return nil, err
	}

	bc, err := platform.NewBaseCluster(opts.Options, rconf, Platform, "")
	if err != nil {
		return nil, err
	}

	oc := &cluster{
		BaseCluster: bc,
		api:         api,
	}

	if !rconf.NoSSHKeyInMetadata {
		keys, err := oc.Keys()
		if err != nil {
			return nil, err
		}
		oc.sshKey = keys[0].String()
	}

	return oc, nil
}

func (oc *cluster) vmname() string {
	b := make([]byte, 5)
	rand.Read(b)
	return fmt.Sprintf("%s-%x", oc.Name()[0:13], b)
}

func (oc *cluster) NewMachine(userdata *conf.UserData) (platform.Machine, error) {
	// the OCI metadata service only exposes the Private IPV4
	conf, err := oc.RenderUserData(userdata, map[string]string{
		"$private_ipv4": "${COREOS_OCI_IPV4_PRIVATE_0}",
	})
	if err != nil {
		return nil, err
	}

	if !conf.IsIgnition() && !conf.IsEmpty() {
		return nil, fmt.Errorf("only Ignition is supported on OCI")
	}

	// coreos-metadata doesn't produce the private ipv4, adding an override which pulls
	// it from the vnics portion fo the metadata service
	conf.AddSystemdUnitDropin("coreos-metadata.service", "oci_override.conf", `[Service]
Type=oneshot
Environment=OUTPUT=/run/metadata/coreos
ExecStart=
ExecStart=/usr/bin/mkdir --parent /run/metadata
ExecStart=/usr/bin/bash -c 'echo "COREOS_OCI_IPV4_PRIVATE_0=$(curl http://169.254.169.254/opc/v1/vnics/ | jq .[].privateIp -r)" > ${OUTPUT}'`)

	instance, err := oc.api.CreateInstance(oc.vmname(), conf.String(), oc.sshKey)
	if err != nil {
		return nil, err
	}

	mach := &machine{
		cluster: oc,
		mach:    instance,
	}

	mach.dir = filepath.Join(oc.RuntimeConf().OutputDir, mach.ID())
	if err := os.Mkdir(mach.dir, 0777); err != nil {
		mach.Destroy()
		return nil, err
	}

	confPath := filepath.Join(mach.dir, "user-data")
	if err := conf.WriteFile(confPath); err != nil {
		mach.Destroy()
		return nil, err
	}

	if mach.journal, err = platform.NewJournal(mach.dir); err != nil {
		mach.Destroy()
		return nil, err
	}

	if err := platform.StartMachine(mach, mach.journal); err != nil {
		mach.Destroy()
		return nil, err
	}

	oc.AddMach(mach)

	return mach, nil
}
