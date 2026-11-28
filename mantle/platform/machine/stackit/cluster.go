// Copyright 2026 Red Hat
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

package stackit

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/platform/api/stackit"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
)

type cluster struct {
	*platform.BaseCluster
	flight     *flight
	networking *clusterNetworking
}

type networkAPI interface {
	CreateNetwork(string) (*stackit.Network, error)
	DeleteNetwork(string) error
	CreateSecurityGroup(string) (*stackit.SecurityGroup, error)
	DeleteSecurityGroup(string) error
}

// Track ownership separately from the IDs selected for a server. User-supplied
// networks and security groups must survive both rollback and normal cleanup.
type clusterNetworking struct {
	api                    networkAPI
	networkID              string
	securityGroups         []string
	managedNetworkID       string
	managedSecurityGroupID string
}

func newClusterNetworking(api networkAPI, name string, opts *stackit.Options) (_ *clusterNetworking, retErr error) {
	networking := &clusterNetworking{
		api: api, networkID: opts.Network, securityGroups: append([]string(nil), opts.SecurityGroups...),
	}
	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, networking.destroy())
		}
	}()
	if networking.networkID == "" {
		network, err := api.CreateNetwork(name)
		if err != nil {
			return nil, err
		}
		networking.networkID = network.ID
		networking.managedNetworkID = network.ID
	}
	if len(networking.securityGroups) == 0 {
		group, err := api.CreateSecurityGroup(name)
		if err != nil {
			return nil, err
		}
		networking.securityGroups = []string{group.ID}
		networking.managedSecurityGroupID = group.ID
	}
	return networking, nil
}

func (n *clusterNetworking) destroy() error {
	var errs []error
	if n.managedSecurityGroupID != "" {
		if err := n.api.DeleteSecurityGroup(n.managedSecurityGroupID); err != nil {
			errs = append(errs, fmt.Errorf("deleting STACKIT cluster security group %s: %w", n.managedSecurityGroupID, err))
		} else {
			n.managedSecurityGroupID = ""
		}
	}
	if n.managedNetworkID != "" {
		if err := n.api.DeleteNetwork(n.managedNetworkID); err != nil {
			errs = append(errs, fmt.Errorf("deleting STACKIT cluster network %s: %w", n.managedNetworkID, err))
		} else {
			n.managedNetworkID = ""
		}
	}
	return errors.Join(errs...)
}

var _ platform.Cluster = (*cluster)(nil)

func (sc *cluster) NewMachine(userdata *conf.UserData) (platform.Machine, error) {
	return sc.NewMachineWithOptions(userdata, platform.MachineOptions{})
}

func (sc *cluster) NewMachineWithOptions(userdata *conf.UserData, options platform.MachineOptions) (platform.Machine, error) {
	if err := options.EnsureNoQEMUOnlyOptions("stackit"); err != nil {
		return nil, err
	}
	if len(options.AdditionalDisks) > 0 {
		return nil, errors.New("platform stackit does not yet support additional disks")
	}
	if options.InstanceType != "" {
		return nil, errors.New("platform stackit does not support changing instance types")
	}

	conf, err := sc.RenderUserData(userdata, nil)
	if err != nil {
		return nil, err
	}

	var keyName string
	if !sc.RuntimeConf().NoSSHKeyInMetadata {
		keyName = sc.flight.Name()
	}
	name := fmt.Sprintf("%s-%d", sc.Name(), sc.AllocateMachineSerial())
	server, err := sc.flight.api.CreateServerWithNetwork(name, keyName, conf.String(), sc.networking.networkID, sc.networking.securityGroups)
	if err != nil {
		return nil, err
	}

	mach := &machine{
		cluster: sc,
		server:  server,
	}

	dir := filepath.Join(sc.RuntimeConf().OutputDir, mach.ID())
	if err := os.Mkdir(dir, 0777); err != nil {
		mach.Destroy()
		return nil, err
	}
	mach.dir = dir
	if err := conf.WriteFile(filepath.Join(dir, "user-data")); err != nil {
		mach.Destroy()
		return nil, err
	}
	if mach.journal, err = platform.NewJournal(dir); err != nil {
		mach.Destroy()
		return nil, err
	}

	// Wait for the instance to boot and become accessible over SSH.
	if err := platform.StartMachine(mach, mach.journal); err != nil {
		mach.Destroy()
		return nil, err
	}

	sc.AddMach(mach)
	return mach, nil
}

func (sc *cluster) Destroy() {
	sc.BaseCluster.Destroy()
	if err := sc.networking.destroy(); err != nil {
		plog.Errorf("Error deleting networking for cluster %v: %v", sc.Name(), err)
		return
	}
	sc.flight.DelCluster(sc)
}
