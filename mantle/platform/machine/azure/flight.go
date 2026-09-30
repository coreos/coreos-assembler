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

	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/platform/api/azure"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
)

const (
	Platform platform.Name = "azure"

	// MetadataSSHKeyComment marks the SSH key kola uploads via Azure's
	// cloud metadata, so tests can confirm a key came from there
	// rather than from Ignition's own config.
	MetadataSSHKeyComment = "core@azure-metadata"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/coreos-assembler/mantle", "platform/machine/azure")
)

type flight struct {
	*platform.BaseFlight
	api    *azure.API
	SSHKey string
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
	// Tag the key uploaded to Azure with a distinct comment (instead
	// of the default "core@default") so tests can identify it. This
	// only changes the comment on the uploaded public key, not the
	// key used for actual SSH auth.
	sshKey := *keys[0]
	sshKey.Comment = MetadataSSHKeyComment
	af.SSHKey = sshKey.String()

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
	}

	ac.ResourceGroup, err = af.api.CreateResourceGroup("kola-cluster")
	if err != nil {
		return nil, err
	}

	ac.StorageAccount, err = af.api.CreateStorageAccount(ac.ResourceGroup)
	if err != nil {
		if e := af.api.TerminateResourceGroup(ac.ResourceGroup); e != nil {
			plog.Errorf("Deleting resource group %v: %v", ac.ResourceGroup, e)
		}
		return nil, err
	}

	_, err = af.api.PrepareNetworkResources(ac.ResourceGroup)
	if err != nil {
		if e := af.api.TerminateResourceGroup(ac.ResourceGroup); e != nil {
			plog.Errorf("Deleting resource group %v: %v", ac.ResourceGroup, e)
		}
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
