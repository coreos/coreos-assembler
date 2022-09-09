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
	"crypto/rand"
	"fmt"
	"strings"
	"time"

	"github.com/coreos/mantle/util"
	"golang.org/x/crypto/ssh/agent"
	"google.golang.org/api/compute/v1"
)

func (a *API) vmname() string {
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		plog.Error(err)
	}
	return fmt.Sprintf("%s-%x", a.options.BaseName, b)
}

// Taken from: https://github.com/golang/build/blob/master/buildlet/gce.go
func (a *API) mkinstance(userdata, name string, keys []*agent.Key, useServiceAcct bool) *compute.Instance {
	mantle := "mantle"
	metadataItems := []*compute.MetadataItems{
		{
			// this should be done with a label instead, but
			// our old vendored Go binding doesn't support those
			Key:   "created-by",
			Value: &mantle,
		},
	}
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

	instancePrefix := "https://www.googleapis.com/compute/v1/projects/" + a.options.Project

	instance := &compute.Instance{
		Name:        name,
		MachineType: instancePrefix + "/zones/" + a.options.Zone + "/machineTypes/" + a.options.MachineType,
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
					SourceImage: a.options.Image,
					DiskType:    "/zones/" + a.options.Zone + "/diskTypes/" + a.options.DiskType,
					DiskSizeGb:  16,
				},
			},
		},
		NetworkInterfaces: []*compute.NetworkInterface{
			{
				AccessConfigs: []*compute.AccessConfig{
					{
						Type: "ONE_TO_ONE_NAT",
						Name: "External NAT",
					},
				},
				Network: instancePrefix + "/global/networks/" + a.options.Network,
			},
		},
	}
	if useServiceAcct {
		// allow the instance to perform authenticated GCS fetches
		instance.ServiceAccounts = []*compute.ServiceAccount{
			{
				Email:  a.options.ServiceAcct,
				Scopes: []string{"https://www.googleapis.com/auth/devstorage.read_only"},
			},
		}
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

// CreateInstance creates a Google Compute Engine instance.
func (a *API) CreateInstance(userdata string, keys []*agent.Key, useServiceAcct bool) (*compute.Instance, error) {
	name := a.vmname()
	inst := a.mkinstance(userdata, name, keys, useServiceAcct)

	plog.Debugf("Creating instance %q", name)

	op, err := a.compute.Instances.Insert(a.options.Project, a.options.Zone, inst).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to request new GCE instance: %v\n", err)
	}

	doable := a.compute.ZoneOperations.Get(a.options.Project, a.options.Zone, op.Name)
	if err := a.NewPending(op.Name, doable).Wait(); err != nil {
		return nil, err
	}

	err = util.WaitUntilReady(10*time.Minute, 10*time.Second, func() (bool, error) {
		var err error
		inst, err = a.compute.Instances.Get(a.options.Project, a.options.Zone, name).Do()
		if err != nil {
			return false, err
		}
		return inst.Status == "RUNNING", nil
	})
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

func (a *API) GetConsoleOutput(name string) (string, error) {
	out, err := a.compute.Instances.GetSerialPortOutput(a.options.Project, a.options.Zone, name).Do()
	if err != nil {
		return "", fmt.Errorf("failed to retrieve console output for %q: %v", name, err)
	}
	return out.Contents, nil
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

func (a *API) gcInstances(gracePeriod time.Duration) error {
	threshold := time.Now().Add(-gracePeriod)

	list, err := a.compute.Instances.List(a.options.Project, a.options.Zone).Do()
	if err != nil {
		return err
	}
	for _, instance := range list.Items {
		// check metadata because our vendored Go binding
		// doesn't support labels
		if instance.Metadata == nil {
			continue
		}
		isMantle := false
		for _, item := range instance.Metadata.Items {
			if item.Key == "created-by" && item.Value != nil && *item.Value == "mantle" {
				isMantle = true
				break
			}
		}
		if !isMantle {
			continue
		}

		created, err := time.Parse(time.RFC3339, instance.CreationTimestamp)
		if err != nil {
			return fmt.Errorf("couldn't parse %q: %v", instance.CreationTimestamp, err)
		}
		if created.After(threshold) {
			continue
		}

		switch instance.Status {
		case "TERMINATED":
			continue
		}

		if err := a.TerminateInstance(instance.Name); err != nil {
			return fmt.Errorf("couldn't terminate instance %q: %v", instance.Name, err)
		}
	}

	return nil
}
