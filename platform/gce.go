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

package platform

import (
	"crypto/rand"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"google.golang.org/api/compute/v1"

	"github.com/coreos/mantle/auth"
	"github.com/coreos/mantle/platform/conf"
)

type GCEOptions struct {
	Image       string
	Project     string
	Zone        string
	MachineType string
	DiskType    string
	Network     string
	ServiceAuth bool
	*Options
}

type gceCluster struct {
	*baseCluster
	conf *GCEOptions
	api  *compute.Service
}

type gceMachine struct {
	gc    *gceCluster
	name  string
	intIP string
	extIP string
}

func NewGCECluster(conf GCEOptions) (Cluster, error) {
	var client *http.Client
	var err error
	if conf.ServiceAuth {
		client = auth.GoogleServiceClient()
	} else {
		client, err = auth.GoogleClient()
	}
	if err != nil {
		return nil, err
	}

	api, err := compute.New(client)
	if err != nil {
		return nil, err
	}

	bc, err := newBaseCluster(conf.BaseName)
	if err != nil {
		return nil, err
	}

	gc := &gceCluster{
		baseCluster: bc,
		api:         api,
		conf:        &conf,
	}

	return gc, nil
}

func (gc *gceCluster) Destroy() error {
	for _, gm := range gc.Machines() {
		gm.Destroy()
	}
	gc.agent.Close()
	return nil
}

// Calling in parallel is ok
func (gc *gceCluster) NewMachine(userdata string) (Machine, error) {
	conf, err := conf.New(userdata)
	if err != nil {
		return nil, err
	}

	keys, err := gc.agent.List()
	if err != nil {
		return nil, err
	}

	conf.CopyKeys(keys)

	// Create gce VM and wait for creation to succeed.
	gm, err := GCECreateVM(gc.api, gc.conf, conf.String(), keys)
	if err != nil {
		return nil, err
	}
	gm.gc = gc

	if err := commonMachineChecks(gm); err != nil {
		gm.Destroy()
		return nil, err
	}

	gc.addMach(gm)

	return Machine(gm), nil
}

func (gm *gceMachine) ID() string {
	return gm.name
}

func (gm *gceMachine) IP() string {
	return gm.extIP
}

func (gm *gceMachine) PrivateIP() string {
	return gm.intIP
}

func (gm *gceMachine) SSHClient() (*ssh.Client, error) {
	return gm.gc.SSHClient(gm.IP())
}

func (gm *gceMachine) PasswordSSHClient(user string, password string) (*ssh.Client, error) {
	return gm.gc.PasswordSSHClient(gm.IP(), user, password)
}

func (gm *gceMachine) SSH(cmd string) ([]byte, error) {
	return gm.gc.SSH(gm, cmd)
}

func (gm *gceMachine) Destroy() error {
	_, err := gm.gc.api.Instances.Delete(gm.gc.conf.Project, gm.gc.conf.Zone, gm.name).Do()
	if err != nil {
		return err
	}

	gm.gc.delMach(gm)

	return nil
}

func GCECreateVM(api *compute.Service, opts *GCEOptions, userdata string, keys []*agent.Key) (*gceMachine, error) {
	// generate name
	name, err := newName(opts)
	if err != nil {
		return nil, fmt.Errorf("Failed allocating unique name for vm: %v\n", err)
	}

	instance, err := gceMakeInstance(opts, userdata, name, keys)
	if err != nil {
		return nil, err
	}

	// request instance
	op, err := api.Instances.Insert(opts.Project, opts.Zone, instance).Do()
	if err != nil {
		return nil, fmt.Errorf("Failed to create new VM: %v\n", err)
	}

	fmt.Fprintf(os.Stderr, "Instance %v requested\n", name)
	fmt.Fprintf(os.Stderr, "Waiting for creation to finish...\n")

	// wait for creation to finish
	err = gceWaitVM(api, opts.Project, opts.Zone, op.Name)
	if err != nil {
		return nil, err
	}

	inst, err := api.Instances.Get(opts.Project, opts.Zone, name).Do()
	if err != nil {
		return nil, fmt.Errorf("Error getting instance %s details after creation: %v", name, err)
	}
	intIP, extIP := instanceIPs(inst)

	gm := &gceMachine{
		name:  name,
		extIP: extIP,
		intIP: intIP,
	}

	return gm, nil
}

func GCEDestroyVM(api *compute.Service, proj, zone, name string) error {
	_, err := api.Instances.Delete(proj, zone, name).Do()
	if err != nil {
		return err
	}
	return nil
}

// Create image on GCE and and wait for completion. Will not overwrite
// existing image.
func GCECreateImage(api *compute.Service, proj, name, source string) error {
	image := &compute.Image{
		Name: name,
		RawDisk: &compute.ImageRawDisk{
			Source: source,
		},
	}

	fmt.Fprintf(os.Stderr, "Image %v requested\n", name)
	fmt.Fprintf(os.Stderr, "Waiting for image creation to finish...\n")

	op, err := api.Images.Insert(proj, image).Do()
	if err != nil {
		return err
	}

	err = gceWaitOp(api, proj, op.Name)
	if err != nil {
		return err
	}
	return nil
}

// Delete image on GCE and then recreate it.
func GCEForceCreateImage(api *compute.Service, proj, name, source string) error {
	// op xor err = nil
	op, err := api.Images.Delete(proj, name).Do()

	if op != nil {
		err = gceWaitOp(api, proj, op.Name)
		if err != nil {
			return err
		}
	}

	// don't return error when delete fails because image doesn't exist
	if err != nil && !strings.HasSuffix(err.Error(), "notFound") {
		return fmt.Errorf("deleting image: %v", err)
	}

	// create
	return GCECreateImage(api, proj, name, source)
}

func GCEListVMs(api *compute.Service, opts *GCEOptions, prefix string) ([]Machine, error) {
	var vms []Machine

	list, err := api.Instances.List(opts.Project, opts.Zone).Do()
	if err != nil {
		return nil, err
	}

	for _, inst := range list.Items {
		if !strings.HasPrefix(inst.Name, prefix) {
			continue
		}
		intIP, extIP := instanceIPs(inst)
		gm := &gceMachine{
			name:  inst.Name,
			extIP: extIP,
			intIP: intIP,
		}

		vms = append(vms, gm)
	}
	return vms, nil
}

func GCEListImages(client *http.Client, proj, prefix string) ([]string, error) {
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

//Some code taken from: https://github.com/golang/build/blob/master/buildlet/gce.go
func gceMakeInstance(opts *GCEOptions, userdata string, name string, keys []*agent.Key) (*compute.Instance, error) {
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

	prefix := "https://www.googleapis.com/compute/v1/projects/" + opts.Project
	instance := &compute.Instance{
		Name:        name,
		MachineType: prefix + "/zones/" + opts.Zone + "/machineTypes/" + opts.MachineType,
		Metadata: &compute.Metadata{
			Items: metadataItems,
		},
		Disks: []*compute.AttachedDisk{
			{
				AutoDelete: true,
				Boot:       true,
				Type:       "PERSISTENT",
				InitializeParams: &compute.AttachedDiskInitializeParams{
					DiskName:    name,
					SourceImage: prefix + "/global/images/" + opts.Image,
					DiskType:    "/zones/" + opts.Zone + "/diskTypes/" + opts.DiskType,
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
				Network: prefix + "/global/networks/" + opts.Network,
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

	return instance, nil
}

//Some code taken from: https://github.com/golang/build/blob/master/buildlet/gce.go
func gceWaitVM(api *compute.Service, proj, zone, opname string) error {
OpLoop:
	for {
		time.Sleep(2 * time.Second)

		op, err := api.ZoneOperations.Get(proj, zone, opname).Do()
		if err != nil {
			return fmt.Errorf("Failed to get op %s: %v", opname, err)
		}
		switch op.Status {
		case "PENDING", "RUNNING":
			continue
		case "DONE":
			if op.Error != nil {
				for _, operr := range op.Error.Errors {
					return fmt.Errorf("Error creating instance: %+v", operr)
				}
				return fmt.Errorf("Failed to start.")
			}
			break OpLoop
		default:
			return fmt.Errorf("Unknown create status %q: %+v", op.Status, op)
		}
	}

	return nil
}

func gceWaitOp(api *compute.Service, proj, opname string) error {
OpLoop:
	for {
		time.Sleep(2 * time.Second)

		op, err := api.GlobalOperations.Get(proj, opname).Do()
		if err != nil {
			return fmt.Errorf("Failed to get op %s: %v", opname, err)
		}
		switch op.Status {
		case "PENDING", "RUNNING":
			continue
		case "DONE":
			if op.Error != nil {
				for _, operr := range op.Error.Errors {
					return fmt.Errorf("Error creating instance: %+v", operr)
				}
				return fmt.Errorf("Failed to start.")
			}
			break OpLoop
		default:
			return fmt.Errorf("Unknown create status %q: %+v", op.Status, op)
		}
	}

	return nil

}

// newName returns a random name prefixed by BaseName
func newName(opts *GCEOptions) (string, error) {
	base := opts.BaseName

	randBytes := make([]byte, 16) //128 bits of entropy
	_, err := rand.Read(randBytes)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%v-%x", base, randBytes), nil
}

// Taken from: https://github.com/golang/build/blob/master/buildlet/gce.go#L323
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
