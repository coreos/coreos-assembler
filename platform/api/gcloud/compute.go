// Copyright 2016 CoreOS, Inc.
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

package gcloud

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	"golang.org/x/crypto/ssh/agent"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"

	"github.com/coreos/mantle/util"
)

func (a *API) vmname() string {
	return fmt.Sprintf("mantle-%x", rand.Int63())
}

// Taken from: https://github.com/golang/build/blob/master/buildlet/gce.go
func (a *API) mkinstance(userdata, name string, keys []*agent.Key) *compute.Instance {
	var metadataItems []*compute.MetadataItems
	if len(keys) > 0 {
		var sshKeys string
		for i, key := range keys {
			sshKeys += fmt.Sprintf("%d:%s\n", i, key)
		}

		metadataItems = append(metadataItems, &compute.MetadataItems{
			Key:   "ssh-keys",
			Value: &sshKeys,
		})
	}

	prefix := "https://www.googleapis.com/compute/v1/projects/" + a.options.Project
	instance := &compute.Instance{
		Name:        name,
		MachineType: prefix + "/zones/" + a.options.Zone + "/machineTypes/" + a.options.MachineType,
		Metadata: &compute.Metadata{
			Items: metadataItems,
		},
		Tags: &compute.Tags{
			// Apparently you need this tag in addition to the
			// firewall rules to open the port because these ports
			// are special?
			Items: []string{"https-server", "http-server"},
		},
		Disks: []*compute.AttachedDisk{
			{
				AutoDelete: true,
				Boot:       true,
				Type:       "PERSISTENT",
				InitializeParams: &compute.AttachedDiskInitializeParams{
					DiskName:    name,
					SourceImage: prefix + "/global/images/" + a.options.Image,
					DiskType:    "/zones/" + a.options.Zone + "/diskTypes/" + a.options.DiskType,
				},
			},
		},
		NetworkInterfaces: []*compute.NetworkInterface{
			&compute.NetworkInterface{
				AccessConfigs: []*compute.AccessConfig{
					&compute.AccessConfig{
						Type: "ONE_TO_ONE_NAT",
						Name: "External NAT",
					},
				},
				Network: prefix + "/global/networks/" + a.options.Network,
			},
		},
	}
	// add cloud config
	if userdata != "" {
		instance.Metadata.Items = append(instance.Metadata.Items, &compute.MetadataItems{
			Key:   "user-data",
			Value: &userdata,
		})
	}

	return instance

}

type doable interface {
	Do(opts ...googleapi.CallOption) (*compute.Operation, error)
}

func (a *API) waitop(operation string, do doable) error {
	retry := func() error {
		op, err := do.Do()
		if err != nil {
			return err
		}

		switch op.Status {
		case "PENDING", "RUNNING":
			return fmt.Errorf("Operation %q is %q", operation, op.Status)
		case "DONE":
			if op.Error != nil {
				for _, operr := range op.Error.Errors {
					return fmt.Errorf("Error creating instance: %+v", operr)
				}
				return fmt.Errorf("Operation %q failed to start", op.Status)
			}

			return nil
		}

		return fmt.Errorf("Unknown operation status %q: %+v", op.Status, op)
	}

	// 5 minutes
	if err := util.Retry(30, 10*time.Second, retry); err != nil {
		return fmt.Errorf("Failed to wait for operation %q: %v", operation, err)
	}

	return nil
}

// CreateInstance creates a Google Compute Engine instance.
func (a *API) CreateInstance(userdata string, keys []*agent.Key) (*compute.Instance, error) {
	name := a.vmname()
	inst := a.mkinstance(userdata, name, keys)

	plog.Debugf("Creating instance %q", name)

	op, err := a.compute.Instances.Insert(a.options.Project, a.options.Zone, inst).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to request new GCE instance: %v\n", err)
	}

	doable := a.compute.ZoneOperations.Get(a.options.Project, a.options.Zone, op.Name)
	if err := a.waitop(op.Name, doable); err != nil {
		return nil, err
	}

	inst, err = a.compute.Instances.Get(a.options.Project, a.options.Zone, name).Do()
	if err != nil {
		return nil, fmt.Errorf("failed getting instance %s details after creation: %v", name, err)
	}

	plog.Debugf("Created instance %q", name)

	return inst, nil
}

func (a *API) TerminateInstance(name string) error {
	plog.Debugf("Terminating instance %q", name)

	_, err := a.compute.Instances.Delete(a.options.Project, a.options.Zone, name).Do()
	return err
}

func (a *API) ListInstances(prefix string) ([]*compute.Instance, error) {
	var instances []*compute.Instance

	list, err := a.compute.Instances.List(a.options.Project, a.options.Zone).Do()
	if err != nil {
		return nil, err
	}

	for _, inst := range list.Items {
		if !strings.HasPrefix(inst.Name, prefix) {
			continue
		}

		instances = append(instances, inst)
	}

	return instances, nil
}

// Taken from: https://github.com/golang/build/blob/master/buildlet/gce.go
func InstanceIPs(inst *compute.Instance) (intIP, extIP string) {
	for _, iface := range inst.NetworkInterfaces {
		if strings.HasPrefix(iface.NetworkIP, "10.") {
			intIP = iface.NetworkIP
		}
		for _, accessConfig := range iface.AccessConfigs {
			if accessConfig.Type == "ONE_TO_ONE_NAT" {
				extIP = accessConfig.NatIP
			}
		}
	}
	return
}
