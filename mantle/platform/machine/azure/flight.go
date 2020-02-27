// Copyright 2018 Red Hat
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

package azure

import (
	"fmt"

	ctplatform "github.com/coreos/container-linux-config-transpiler/config/platform"
	"github.com/coreos/pkg/capnslog"

	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/api/azure"
)

const (
	Platform platform.Name = "azure"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "platform/machine/azure")
)

type flight struct {
	*platform.BaseFlight
	api        *azure.API
	SSHKey     string
	FakeSSHKey string
}

// NewFlight creates an instance of a Flight suitable for spawning
// instances on the Azure platform.
func NewFlight(opts *azure.Options) (platform.Flight, error) {
	api, err := azure.New(opts)
	if err != nil {
		return nil, err
	}

	if err = api.SetupClients(); err != nil {
		return nil, fmt.Errorf("setting up clients: %v", err)
	}

	bf, err := platform.NewBaseFlight(opts.Options, Platform, ctplatform.Azure)
	if err != nil {
		return nil, err
	}

	af := &flight{
		BaseFlight: bf,
		api:        api,
	}

	keys, err := af.Keys()
	if err != nil {
		af.Destroy()
		return nil, err
	}
	af.SSHKey = keys[0].String()
	af.FakeSSHKey, err = platform.GenerateFakeKey()
	if err != nil {
		return nil, err
	}

	return af, nil
}

// NewCluster creates an instance of a Cluster suitable for spawning
// instances on the Azure platform.
func (af *flight) NewCluster(rconf *platform.RuntimeConfig) (platform.Cluster, error) {
	bc, err := platform.NewBaseCluster(af.BaseFlight, rconf)
	if err != nil {
		return nil, err
	}

	ac := &cluster{
		BaseCluster: bc,
		flight:      af,
	}

	if !rconf.NoSSHKeyInMetadata {
		ac.sshKey = af.SSHKey
	} else {
		ac.sshKey = af.FakeSSHKey
	}

	ac.ResourceGroup, err = af.api.CreateResourceGroup("kola-cluster")
	if err != nil {
		return nil, err
	}

	ac.StorageAccount, err = af.api.CreateStorageAccount(ac.ResourceGroup)
	if err != nil {
		return nil, err
	}

	_, err = af.api.PrepareNetworkResources(ac.ResourceGroup)
	if err != nil {
		return nil, err
	}

	af.AddCluster(ac)

	return ac, nil
}

func (af *flight) Destroy() {
	af.BaseFlight.Destroy()
}
