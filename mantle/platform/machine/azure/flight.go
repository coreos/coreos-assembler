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
	"github.com/coreos/pkg/capnslog"
	"github.com/pkg/errors"

	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/api/azure"
	"github.com/coreos/mantle/platform/conf"
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
		return nil, errors.Wrapf(err, "setting up clients")
	}

	bf, err := platform.NewBaseFlight(opts.Options, Platform)
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

	// We have worked around the golang library limitation for
	// keyexchange algorithm by switching to an ecdsa key in
	// network/ssh.go.  However, Azure requires an RSA key.  For
	// now (until we get an updated golang library) we'll just
	// satisfy the requirement by using a fake key and disabling
	// the fcos.ignition.misc.empty and fcos.ignition.v3.noop
	// tests on Azure.
	// https://github.com/coreos/coreos-assembler/issues/1772
	// https://docs.microsoft.com/en-us/azure/virtual-machines/linux/mac-create-ssh-keys#supported-ssh-key-formats
	ac.sshKey = af.FakeSSHKey
	//ac.sshKey = af.SSHKey

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

func (af *flight) ConfigTooLarge(ud conf.UserData) bool {

	// not implemented
	return false
}

func (af *flight) Destroy() {
	af.BaseFlight.Destroy()
}
