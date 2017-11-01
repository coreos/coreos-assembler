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
	"encoding/base64"
	"fmt"
	"time"

	"github.com/oracle/bmcs-go-sdk"

	"github.com/coreos/mantle/util"
)

func (a *API) CreateInstance(name, userdata, sshKey string) (*Machine, error) {
	vcn, err := a.GetVCN("kola")
	if err != nil {
		return nil, err
	}

	subnet, err := a.getSubnetOnVCN(vcn.ID)
	if err != nil {
		return nil, err
	}

	metadata := map[string]string{
		"created_by": "mantle",
	}
	if userdata != "" {
		metadata["user_data"] = base64.StdEncoding.EncodeToString([]byte(userdata))
	}
	if sshKey != "" {
		metadata["ssh_authorized_keys"] = sshKey
	}

	opts := baremetal.LaunchInstanceOptions{
		Metadata: metadata,
		CreateVnicOptions: &baremetal.CreateVnicOptions{
			AssignPublicIp: boolToPtr(true),
			SubnetID:       subnet.ID,
			HostnameLabel:  name,
		},
		HostnameLabel: name, // hostname label must match vnic hostname label
		CreateOptions: baremetal.CreateOptions{
			DisplayNameOptions: baremetal.DisplayNameOptions{
				DisplayName: name,
			},
		},
	}

	inst, err := a.client.LaunchInstance(subnet.AvailabilityDomain, a.opts.CompartmentID, a.opts.Image, a.opts.Shape, subnet.ID, &opts)
	if err != nil {
		return nil, err
	}

	id := inst.ID

	err = util.WaitUntilReady(5*time.Minute, 10*time.Second, func() (bool, error) {
		inst, err = a.client.GetInstance(id)
		if err != nil {
			return false, err
		}

		if inst.State != "RUNNING" {
			return false, nil
		}

		return true, nil
	})
	if err != nil {
		a.TerminateInstance(id)
		return nil, fmt.Errorf("waiting for machine to become active: %v", err)
	}

	vnicOpts := baremetal.ListVnicAttachmentsOptions{
		InstanceIDListOptions: baremetal.InstanceIDListOptions{
			InstanceID: inst.ID,
		},
	}
	vnicAttachments, err := a.client.ListVnicAttachments(a.opts.CompartmentID, &vnicOpts)
	if err != nil {
		a.TerminateInstance(inst.ID)
		return nil, fmt.Errorf("listing vnic attachments: %v", err)
	}

	if len(vnicAttachments.Attachments) < 1 {
		a.TerminateInstance(inst.ID)
		return nil, fmt.Errorf("couldn't get VM information")
	}
	vnic, err := a.client.GetVnic(vnicAttachments.Attachments[0].VnicID)
	if err != nil {
		return nil, fmt.Errorf("getting vnic: %v", err)
	}

	return &Machine{
		Name:             inst.DisplayName,
		ID:               inst.ID,
		PublicIPAddress:  vnic.PublicIPAddress,
		PrivateIPAddress: vnic.PrivateIPAddress,
	}, nil
}

func (a *API) TerminateInstance(instanceID string) error {
	return a.client.TerminateInstance(instanceID, nil)
}

// ConsoleHistory is deleted when an instance is terminated, as such
// we just return errors and let the history be deleted when the instance
// is terminated.
func (a *API) GetConsoleOutput(instanceID string) (string, error) {
	metadata, err := a.client.CaptureConsoleHistory(instanceID, nil)
	if err != nil {
		return "", fmt.Errorf("capturing console history: %v", err)
	}

	consoleHistoryStatus, err := a.client.GetConsoleHistory(metadata.ID)
	if err != nil {
		return "", fmt.Errorf("getting console history status: %v", err)
	}

	err = util.WaitUntilReady(5*time.Minute, 10*time.Second, func() (bool, error) {
		consoleHistoryStatus, err = a.client.GetConsoleHistory(metadata.ID)
		if err != nil {
			return false, fmt.Errorf("getting console history status: %v", err)
		}

		if consoleHistoryStatus.State != "SUCCEEDED" {
			return false, nil
		}

		return true, nil
	})
	if err != nil {
		return "", fmt.Errorf("waiting for console history data to be ready: %v", err)
	}

	// OCI limits console history to 1 MB; request 2 to be safe
	content, err := a.client.ShowConsoleHistoryData(consoleHistoryStatus.ID, &baremetal.ConsoleHistoryDataOptions{
		Length: 2 * 1024 * 1024,
		Offset: 0,
	})
	if err != nil {
		return "", fmt.Errorf("getting console history data: %v", err)
	}

	return content.Data, nil
}

func (a *API) gcInstances(gracePeriod time.Duration) error {
	threshold := time.Now().Add(-gracePeriod)

	result, err := a.client.ListInstances(a.opts.CompartmentID, nil)
	if err != nil {
		return err
	}
	for _, instance := range result.Instances {
		if instance.Metadata["created_by"] != "mantle" {
			continue
		}

		if instance.TimeCreated.After(threshold) {
			continue
		}

		switch instance.State {
		case "TERMINATING", "TERMINATED":
			continue
		}

		if err := a.TerminateInstance(instance.ID); err != nil {
			return fmt.Errorf("couldn't terminate instance %v: %v", instance.ID, err)
		}
	}
	return nil
}
