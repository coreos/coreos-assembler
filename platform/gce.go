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
	"bytes"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/coreos/coreos-cloudinit/config"
	"github.com/coreos/mantle/Godeps/_workspace/src/golang.org/x/crypto/ssh"
	"github.com/coreos/mantle/auth"
	"github.com/coreos/mantle/platform/local"
	"google.golang.org/api/compute/v1"
)

var (
	gceImage       = flag.String("gce.image", "", "GCE image")
	gceProject     = flag.String("gce.project", "coreos-gce-testing", "GCE project")
	gceZone        = flag.String("gce.zone", "us-central1-a", "GCE zone")
	gceMachineType = flag.String("gce.machine", "n1-standard-1", "GCE machine type")
	gceDisk        = flag.String("gce.disk", "pd-ssd", "GCE disk type")
	gceBaseName    = flag.String("gce.basename", "kola", "GCE instance names will be generated from this")
	gceNetwork     = flag.String("gce.network", "default", "GCE network")
)

type gceCluster struct {
	*local.LocalCluster
	machines map[string]*gceMachine
}

type gceMachine struct {
	gc          *gceCluster
	name        string
	intIP       string
	extIP       string
	cloudConfig string
	sshClient   *ssh.Client
	opts        *GCEOpts
}

type GCEOpts struct {
	Client      *http.Client
	CloudConfig string
	Image       string
	Project     string
	Zone        string
	MachineType string
	DiskType    string
	BaseName    string
	Network     string
}

// fills in defaults for unset fields and error for required fields
func (opts *GCEOpts) setDefaults() error {
	if opts.Client == nil {
		return fmt.Errorf("Client is nil")
	}
	if opts.Image == "" {
		return fmt.Errorf("Image not specified")
	}
	if opts.Project == "" {
		opts.Project = *gceProject
	}
	if opts.Zone == "" {
		opts.Zone = *gceZone
	}
	if opts.MachineType == "" {
		opts.MachineType = *gceMachineType
	}
	if opts.DiskType == "" {
		opts.DiskType = *gceDisk
	}
	if opts.BaseName == "" {
		opts.BaseName = *gceBaseName
	}
	if opts.Network == "" {
		opts.Network = *gceNetwork
	}
	return nil
}

func NewGCECluster() (Cluster, error) {
	gc := &gceCluster{
		machines: make(map[string]*gceMachine),
	}
	return Cluster(gc), nil
}

func (gc *gceCluster) Machines() []Machine {
	machines := make([]Machine, 0, len(gc.machines))
	for _, m := range gc.machines {
		machines = append(machines, m)
	}
	return machines
}

func (gc *gceCluster) Destroy() error {
	for _, gm := range gc.machines {
		gm.Destroy()
	}
	return nil
}

func (gc *gceCluster) NewMachine(cloudConfig string) (Machine, error) {
	client, err := auth.GoogleClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Authentication failed: %v\n", err)
		return nil, err
	}

	cconfig, err := config.NewCloudConfig(cloudConfig)
	if err != nil {
		return nil, err
	}
	if err = gc.SSHAgent.UpdateConfig(cconfig); err != nil {
		return nil, err
	}
	cloudConfig = cconfig.String()

	opts := &GCEOpts{CloudConfig: cloudConfig, Client: client, Image: *gceImage}

	// Create gce VM and wait for creation to succeed.
	gm, err := GCECreateVM(opts)
	if err != nil {
		return nil, err
	}

	err = sshCheck(gm)
	if err != nil {
		gm.Destroy()
		return nil, err
	}

	gc.machines[gm.ID()] = gm
	return Machine(gm), nil
}

func (gm *gceMachine) ID() string {
	return gm.name
}

func (gm *gceMachine) IP() string {
	return gm.extIP
}

func (gm *gceMachine) SSHSession() (*ssh.Session, error) {
	session, err := gm.sshClient.NewSession()
	if err != nil {
		return nil, err
	}

	return session, nil
}

func (gm *gceMachine) SSH(cmd string) ([]byte, error) {
	session, err := gm.SSHSession()
	if err != nil {
		return []byte{}, err
	}
	defer session.Close()

	session.Stderr = os.Stderr
	out, err := session.Output(cmd)
	out = bytes.TrimSpace(out)
	return out, err
}

func (gm *gceMachine) StartJournal() error {
	s, err := gm.SSHSession()
	if err != nil {
		return fmt.Errorf("SSH session failed: %v", err)
	}

	s.Stdout = os.Stdout
	s.Stderr = os.Stderr
	go func() {
		s.Run("journalctl -f")
		s.Close()
	}()

	return nil
}

func (gm *gceMachine) Destroy() error {
	if gm.sshClient != nil {
		gm.sshClient.Close()
	}
	if gm.opts.Client == nil {
		return fmt.Errorf("gce Machine has nil client, cannot destroy")
	}

	computeService, err := compute.New(gm.opts.Client)
	if err != nil {
		return err
	}
	_, err = computeService.Instances.Delete(gm.opts.Project, gm.opts.Zone, gm.name).Do()
	if err != nil {
		return err
	}

	delete(gm.gc.machines, gm.ID())
	return nil
}

func GCECreateVM(opts *GCEOpts) (*gceMachine, error) {
	err := opts.setDefaults()
	if err != nil {
		return nil, err
	}

	// check cloud config
	if opts.CloudConfig != "" {
		_, err := config.NewCloudConfig(opts.CloudConfig)
		if err != nil {
			return nil, err
		}
	}

	// generate name
	name, err := nextName(opts.Client, opts.Project, opts.Zone, opts.BaseName)
	if err != nil {
		return nil, fmt.Errorf("Failed allocating unique name for vm: %v\n", err)
	}

	instance, err := gceMakeInstance(opts, name)
	if err != nil {
		return nil, err
	}

	computeService, err := compute.New(opts.Client)
	if err != nil {
		return nil, err
	}

	// request instance
	op, err := computeService.Instances.Insert(opts.Project, opts.Zone, instance).Do()
	if err != nil {
		return nil, fmt.Errorf("Failed to create new VM: %v\n", err)
	}

	fmt.Printf("Instance %v requested\n", name)
	fmt.Printf("Waiting for creation to finish...\n")

	// wait for creation to finish
	err = gceWaitVM(computeService, opts.Project, opts.Zone, op.Name)
	if err != nil {
		return nil, err
	}

	inst, err := computeService.Instances.Get(opts.Project, opts.Zone, name).Do()
	if err != nil {
		return nil, fmt.Errorf("Error getting instance %s details after creation: %v", name, err)
	}
	intIP, extIP := instanceIPs(inst)

	gm := &gceMachine{
		name:        name,
		extIP:       extIP,
		intIP:       intIP,
		cloudConfig: opts.CloudConfig,
		opts:        opts,
	}

	return gm, nil
}

func GCEDestroyVM(client *http.Client, proj, zone, name string) error {
	computeService, err := compute.New(client)
	if err != nil {
		return err
	}
	_, err = computeService.Instances.Delete(proj, zone, name).Do()
	if err != nil {
		return err
	}
	return nil
}

// Create image on GCE and return. Will not overwrite existing image.
func GCECreateImage(client *http.Client, proj, name, source string) error {
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
func GCEForceCreateImage(client *http.Client, proj, name, source string) error {
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
	return GCECreateImage(client, proj, name, source)
}

func GCEListVMs(client *http.Client, proj, zone, prefix string) ([]Machine, error) {
	var vms []Machine

	computeService, err := compute.New(client)
	if err != nil {
		return nil, err
	}

	list, err := computeService.Instances.List(proj, zone).Do()
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
func gceMakeInstance(opts *GCEOpts, name string) (*compute.Instance, error) {
	prefix := "https://www.googleapis.com/compute/v1/projects/" + opts.Project
	instance := &compute.Instance{
		Name:        name,
		MachineType: prefix + "/zones/" + opts.Zone + "/machineTypes/" + opts.MachineType,
		Metadata:    &compute.Metadata{},
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
	if opts.CloudConfig != "" {
		instance.Metadata.Items = append(instance.Metadata.Items, &compute.MetadataItems{
			Key:   "user-data",
			Value: opts.CloudConfig,
		})
	}

	return instance, nil
}

//Some code taken from: https://github.com/golang/build/blob/master/buildlet/gce.go
func gceWaitVM(service *compute.Service, proj, zone, opname string) error {
OpLoop:
	for {
		time.Sleep(2 * time.Second)

		op, err := service.ZoneOperations.Get(proj, zone, opname).Do()
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

// nextName returns the next available numbered name or the given base
// name. Code originally from:
// https://github.com/golang/build/blob/master/cmd/gomote/list.go
func nextName(client *http.Client, proj, zone, base string) (string, error) {
	vms, err := GCEListVMs(client, proj, zone, base)
	if err != nil {
		return "", fmt.Errorf("error listing VMs: %v", err)
	}
	matches := map[string]bool{}
	for _, vm := range vms {
		if strings.HasPrefix(vm.ID(), base) {
			matches[vm.ID()] = true
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

func sshCheck(gm *gceMachine) error {
	// Allow a few authentication failures in case setup is slow.
	var err error
	for i := 0; i < sshRetries; i++ {
		gm.sshClient, err = gm.gc.SSHAgent.NewClient(gm.IP())
		if err != nil {
			fmt.Fprintf(os.Stderr, "ssh error: %v\n", err)
			time.Sleep(sshRetryDelay)
		} else {
			break
		}
	}
	if err != nil {
		return err
	}

	// sanity check
	out, err := gm.SSH("grep ^ID= /etc/os-release")
	if err != nil {
		return err
	}

	if !bytes.Equal(out, []byte("ID=coreos")) {
		return fmt.Errorf("Unexpected SSH output: %s", out)
	}

	return nil
}
