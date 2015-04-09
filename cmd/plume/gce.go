// Copyright 2015 CoreOS, Inc.
// Copyright 2015 The Go Authors.
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

package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/mantle/Godeps/_workspace/src/google.golang.org/api/compute/v1"
)

type gceVM struct {
	name  string
	intIP string
	extIP string
}

func (vm gceVM) String() string {
	s := fmt.Sprintf("%v:\n", vm.name)
	s += fmt.Sprintf(" int: %v\n", vm.intIP)
	s += fmt.Sprintf(" ext: %v\n", vm.extIP)
	return s
}

// Create image on GCE and return. Will not overwrite existing image.
func createImage(client *http.Client, proj, name, source string) error {
	computeService, err := compute.New(client)
	if err != nil {
		return err
	}

	image := &compute.Image{
		Name: name,
		RawDisk: &compute.ImageRawDisk{
			Source: source,
		},
	}
	_, err = computeService.Images.Insert(proj, image).Do()
	if err != nil {
		return err
	}
	return nil
}

// Delete image on GCE and then recreate it.
func forceCreateImage(client *http.Client, proj, name, source string) error {
	// delete
	computeService, err := compute.New(client)
	if err != nil {
		return fmt.Errorf("deleting image: %v", err)
	}
	_, err = computeService.Images.Delete(proj, name).Do()
	if err != nil {
		return fmt.Errorf("deleting image: %v", err)
	}

	// create
	return createImage(client, proj, name, source)
}

func listVMs(client *http.Client, proj, zone, prefix string) ([]gceVM, error) {
	var vms []gceVM
	computeService, err := compute.New(client)
	list, err := computeService.Instances.List(proj, zone).Do()
	if err != nil {
		return nil, err
	}

	for _, inst := range list.Items {
		if !strings.HasPrefix(inst.Name, prefix) {
			continue
		}
		intIP, extIP := instanceIPs(inst)
		vm := gceVM{
			name:  inst.Name,
			extIP: extIP,
			intIP: intIP,
		}
		vms = append(vms, vm)
	}
	return vms, nil
}

func listImages(client *http.Client, proj, prefix string) ([]string, error) {
	var images []string
	computeService, err := compute.New(client)
	list, err := computeService.Images.List(proj).Do()
	if err != nil {
		return nil, err
	}

	for _, image := range list.Items {
		if !strings.HasPrefix(image.Name, prefix) {
			continue
		}
		images = append(images, image.Name)
	}
	return images, nil
}

// create gce VM and wait for creation to succeed. Some code snipped from:
// https://github.com/golang/build/blob/master/buildlet/gce.go#L323
func createVM(client *http.Client, proj, zone, machineType, name, imageName, cfg string) (*gceVM, error) {
	computeService, err := compute.New(client)
	if err != nil {
		return nil, err
	}

	prefix := "https://www.googleapis.com/compute/v1/projects/" + proj
	machType := prefix + "/zones/" + zone + "/machineTypes/" + machineType
	diskType := "https://www.googleapis.com/compute/v1/projects/" + proj + "/zones/" + zone + "/diskTypes/pd-ssd"
	// uses default network which for coreos-gce-testing has open ports for ssh and etcd
	instance := &compute.Instance{
		Name:        name,
		MachineType: machType,
		Metadata:    &compute.Metadata{},
		Disks: []*compute.AttachedDisk{
			{
				AutoDelete: true,
				Boot:       true,
				Type:       "PERSISTENT",
				InitializeParams: &compute.AttachedDiskInitializeParams{
					DiskName:    name,
					SourceImage: "https://www.googleapis.com/compute/v1/projects/" + proj + "/global/images/" + imageName,
					DiskType:    diskType,
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
				Network: prefix + "/global/networks/default",
			},
		},
	}
	// add cloud config
	if cfg != "" {
		instance.Metadata.Items = append(instance.Metadata.Items, &compute.MetadataItems{
			Key:   "user-data",
			Value: cfg,
		})
	}

	op, err := computeService.Instances.Insert(proj, zone, instance).Do()
	if err != nil {
		return nil, fmt.Errorf("Failed to create new VM: %v\n", err)
	}
	fmt.Printf("Instance %v requested\n", name)
	fmt.Printf("Waiting for creation to finish...\n")

	// wait for creation to finish
OpLoop:
	for {
		time.Sleep(2 * time.Second)

		op, err := computeService.ZoneOperations.Get(proj, zone, op.Name).Do()
		if err != nil {
			return nil, fmt.Errorf("Failed to get op %s: %v", op.Name, err)
		}
		switch op.Status {
		case "PENDING", "RUNNING":
			continue
		case "DONE":
			if op.Error != nil {
				for _, operr := range op.Error.Errors {
					return nil, fmt.Errorf("Error creating instance: %+v", operr)
				}
				return nil, fmt.Errorf("Failed to start.")
			}
			break OpLoop
		default:
			return nil, fmt.Errorf("Unknown create status %q: %+v", op.Status, op)
		}
	}

	inst, err := computeService.Instances.Get(proj, zone, name).Do()
	if err != nil {
		return nil, fmt.Errorf("Error getting instance %s details after creation: %v", name, err)
	}

	intIP, extIP := instanceIPs(inst)
	vm := &gceVM{
		name:  name,
		extIP: extIP,
		intIP: intIP,
	}

	return vm, nil
}

func destroyVM(client *http.Client, proj, zone, name string) error {
	computeService, err := compute.New(client)
	if err != nil {
		return err
	}
	_, err = computeService.Instances.Delete(proj, zone, name).Do()
	return err
}

// snipped from: https://github.com/golang/build/blob/master/buildlet/gce.go#L323
func instanceIPs(inst *compute.Instance) (intIP, extIP string) {
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

// nextName returns the next available numbered name or the given base name.
// snipped from: https://github.com/golang/build/blob/master/cmd/gomote/list.go
func nextName(client *http.Client, proj, zone, base string) (string, error) {
	vms, err := listVMs(client, proj, zone, base)
	if err != nil {
		return "", fmt.Errorf("error listing VMs: %v", err)
	}
	matches := map[string]bool{}
	for _, vm := range vms {
		if strings.HasPrefix(vm.name, base) {
			matches[vm.name] = true
		}
	}
	if len(matches) == 0 || !matches[base] {
		return base, nil
	}
	for i := 1; ; i++ {
		next := fmt.Sprintf("%v-%v", base, i)
		if !matches[next] {
			return next, nil
		}
	}
}
