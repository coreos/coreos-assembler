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
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/util"
	"golang.org/x/crypto/ssh/agent"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
)

func (a *API) vmname() string {
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		plog.Error(err)
	}
	return fmt.Sprintf("%s-%x", a.options.BaseName, b)
}

// ["5G:channel=nvme"], by default the disk type is local-ssd
func ParseDisk(spec string, zone string) (*compute.AttachedDisk, error) {
	var diskInterface string

	size, diskmap, err := util.ParseDiskSpec(spec, false)
	if err != nil {
		return nil, fmt.Errorf("failed to parse disk spec %q: %w", spec, err)
	}
	for key, value := range diskmap {
		switch key {
		case "channel":
			switch value {
			case "nvme", "scsi":
				diskInterface = strings.ToUpper(value)
			default:
				return nil, fmt.Errorf("invalid channel value: %q", value)
			}
		default:
			return nil, fmt.Errorf("invalid key %q", key)
		}
	}

	return &compute.AttachedDisk{
		AutoDelete: true,
		Boot:       false,
		Type:       "SCRATCH",
		Interface:  diskInterface,
		InitializeParams: &compute.AttachedDiskInitializeParams{
			DiskType:   "/zones/" + zone + "/diskTypes/local-ssd",
			DiskSizeGb: size,
		},
	}, nil
}

// Taken from: https://github.com/golang/build/blob/master/buildlet/gce.go
func (a *API) mkinstance(userdata, name, zone string, keys []*agent.Key, opts platform.MachineOptions, useServiceAcct bool) (*compute.Instance, error) {
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
		MachineType: instancePrefix + "/zones/" + zone + "/machineTypes/" + a.options.MachineType,
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
					DiskType:    "/zones/" + zone + "/diskTypes/" + a.options.DiskType,
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
	// create confidential instance
	if a.options.ConfidentialType != "" {
		ConfidentialType := strings.ToUpper(a.options.ConfidentialType)
		ConfidentialType = strings.Replace(ConfidentialType, "-", "_", -1)
		if ConfidentialType == "SEV" || ConfidentialType == "SEV_SNP" || ConfidentialType == "TDX" {
			fmt.Printf("Using confidential type for confidential computing %s\n", ConfidentialType)
			instance.ConfidentialInstanceConfig = &compute.ConfidentialInstanceConfig{
				ConfidentialInstanceType: ConfidentialType,
			}
			instance.Scheduling = &compute.Scheduling{
				OnHostMaintenance: "TERMINATE",
			}
		} else {
			return nil, fmt.Errorf("Does not support confidential type %s, should be: sev, sev_snp, tdx\n", a.options.ConfidentialType)
		}
	}
	// metal instances can only have a TERMINATE maintenance policy
	if strings.HasSuffix(a.options.MachineType, "metal") {
		instance.Scheduling = &compute.Scheduling{
			OnHostMaintenance: "TERMINATE",
		}
	}
	// attach aditional disk
	for _, spec := range opts.AdditionalDisks {
		plog.Debugf("Parsing disk spec %q\n", spec)
		disk, err := ParseDisk(spec, zone)
		if err != nil {
			return nil, fmt.Errorf("failed to parse spec %q: %w", spec, err)
		}
		instance.Disks = append(instance.Disks, disk)
	}
	return instance, nil
}

var zoneUnavailableErrorPattern = regexp.MustCompile("is currently unavailable in the .+ zone")

// isZoneError returns true if the error is due to the zone (i.e. zone has no resources) and false
// if the error is not due to zone issues.
// See: https://docs.cloud.google.com/compute/docs/troubleshooting/troubleshooting-resource-availability
func isZoneError(errorMessage string) bool {
	return strings.Contains(errorMessage, "does not have enough resources available") ||
		zoneUnavailableErrorPattern.MatchString(errorMessage)
}

// isRetriableError returns true if it could be a flake / network error that may be worth retrying
// The error codes that are considered transient are those defined in: https://docs.cloud.google.com/storage/docs/retry-strategy
func isRetriableError(err error) bool {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return true
	}

	// If we got a googleapi error, then we will retry on 408, 429, and 5xx response codes.
	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) {
		return apiErr.Code == 408 || apiErr.Code == 429 ||
			(apiErr.Code >= 500 && apiErr.Code <= 599)
	}

	return false
}

// CreateInstance creates a Google Compute Engine instance. It will first attempt to create it in the zone given by a.options.PreferredZone, if
// that fails (due to for example a capacity error) it will fall back to other zones in the same region.
func (a *API) CreateInstance(userdata string, keys []*agent.Key, opts platform.MachineOptions, useServiceAcct bool) (*compute.Instance, error) {
	var lastError error
	for _, zone := range a.zones {
		name := a.vmname() // we need a different name for each try
		inst, err := a.mkinstance(userdata, name, zone, keys, opts, useServiceAcct)
		if err != nil {
			return nil, fmt.Errorf("failed to create instance %q: %w", name, err)
		}

		plog.Debugf("Creating instance %q in zone %s", name, zone)

		// Lets try to insert the instance, and retry if we get a network issue
		var op *compute.Operation
		err = util.RetryConditional(3, 10*time.Second, isRetriableError, func() error {
			op, err = a.compute.Instances.Insert(a.options.Project, zone, inst).Do()
			if err != nil {
				return fmt.Errorf("failed to request new GCP instance: %w", err)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}

		doable := a.compute.ZoneOperations.Get(a.options.Project, zone, op.Name)
		if err := a.NewPending(op.Name, doable).Wait(); err != nil {
			plog.Warningf("Failed to create instance %q in zone %s: %v", name, zone, err)
			lastError = err
			// If the error is caused by the zone we chose, then lets continue and try a different zone
			if isZoneError(err.Error()) {
				continue
			} else {
				break
			}
		}

		err = util.WaitUntilReady(10*time.Minute, 10*time.Second, func() (bool, error) {
			var err error
			inst, err = a.compute.Instances.Get(a.options.Project, zone, name).Do()
			if err != nil {
				if isRetriableError(err) {
					plog.Warningf("error getting instance %s in zone %s, will retry: %v", name, zone, err)
					return false, nil
				}
				return false, err
			}
			return inst.Status == "RUNNING", nil
		})

		if err != nil {
			return nil, fmt.Errorf("failed getting instance %s details after creation: %w", name, err)
		}

		plog.Debugf("Created instance %q in zone %s", name, zone)
		return inst, nil
	}

	return nil, fmt.Errorf("failed to create instance in all zones: %w", lastError)
}

func (a *API) TerminateInstance(name, zone string) error {
	plog.Debugf("Terminating instance %q", name)

	_, err := a.compute.Instances.Delete(a.options.Project, zone, name).Do()
	return err
}

func (a *API) ListInstances(prefix string) ([]*compute.Instance, error) {
	var instances []*compute.Instance

	for _, zone := range a.zones {
		list, err := a.compute.Instances.List(a.options.Project, zone).Do()
		if err != nil {
			return nil, err
		}

		for _, inst := range list.Items {
			if !strings.HasPrefix(inst.Name, prefix) {
				continue
			}

			instances = append(instances, inst)
		}
	}

	return instances, nil
}

func (a *API) GetConsoleOutput(name, zone string) (string, error) {
	out, err := a.compute.Instances.GetSerialPortOutput(a.options.Project, zone, name).Do()
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

func (a *API) gcInstances(gracePeriod time.Duration, zone string) error {
	threshold := time.Now().Add(-gracePeriod)

	list, err := a.compute.Instances.List(a.options.Project, zone).Do()
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

		if err := a.TerminateInstance(instance.Name, zone); err != nil {
			return fmt.Errorf("couldn't terminate instance %q: %v", instance.Name, err)
		}
	}

	return nil
}
