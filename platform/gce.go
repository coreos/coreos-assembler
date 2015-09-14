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
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/coreos/coreos-cloudinit/config"
	"github.com/coreos/mantle/Godeps/_workspace/src/golang.org/x/crypto/ssh"
	"github.com/coreos/mantle/Godeps/_workspace/src/google.golang.org/api/compute/v1"
	"github.com/coreos/mantle/auth"
	"github.com/coreos/mantle/network"
	"github.com/coreos/mantle/util"
)

type GCEOptions struct {
	Image       string
	Project     string
	Zone        string
	MachineType string
	DiskType    string
	BaseName    string
	Network     string
}

type gceCluster struct {
	api      *compute.Service
	sshAgent *network.SSHAgent
	conf     *GCEOptions
	machines map[string]*gceMachine
}

type gceMachine struct {
	gc        *gceCluster
	name      string
	intIP     string
	extIP     string
	sshClient *ssh.Client
}

func NewGCECluster(conf GCEOptions) (Cluster, error) {
	client, err := auth.GoogleClient()
	if err != nil {
		return nil, err
	}

	api, err := compute.New(client)
	if err != nil {
		return nil, err
	}

	gc := &gceCluster{
		api:      api,
		conf:     &conf,
		machines: make(map[string]*gceMachine),
	}

	gc.sshAgent, err = network.NewSSHAgent(&net.Dialer{})
	if err != nil {
		return nil, err
	}

	return gc, nil
}

func (gc *gceCluster) NewCommand(name string, arg ...string) util.Cmd {
	return util.NewCommand(name, arg...)
}

func (gc *gceCluster) EtcdEndpoint() string {
	return ""
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
	gc.sshAgent.Close()
	return nil
}

func (gc *gceCluster) NewMachine(cloudConfig string) (Machine, error) {
	cconfig, err := config.NewCloudConfig(cloudConfig)
	if err != nil {
		return nil, err
	}
	if err = gc.sshAgent.UpdateConfig(cconfig); err != nil {
		return nil, err
	}
	cloudConfig = cconfig.String()

	// Create gce VM and wait for creation to succeed.
	gm, err := GCECreateVM(gc.api, gc.conf, cloudConfig)
	if err != nil {
		return nil, err
	}
	gm.gc = gc

	err = sshCheck(gm)
	if err != nil {
		gm.Destroy()
		return nil, err
	}

	gc.machines[gm.ID()] = gm
	return Machine(gm), nil
}

func (gce *gceCluster) GetDiscoveryURL(size int) (string, error) {
	resp, err := http.Get(fmt.Sprintf("https://discovery.etcd.io/new?size=%v", size))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
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

	_, err := gm.gc.api.Instances.Delete(gm.gc.conf.Project, gm.gc.conf.Zone, gm.name).Do()
	if err != nil {
		return err
	}

	delete(gm.gc.machines, gm.ID())
	return nil
}

func GCECreateVM(api *compute.Service, opts *GCEOptions, userdata string) (*gceMachine, error) {
	if userdata != "" {
		_, err := config.NewCloudConfig(userdata)
		if err != nil {
			return nil, err
		}
	}

	// generate name
	name, err := nextName(api, opts)
	if err != nil {
		return nil, fmt.Errorf("Failed allocating unique name for vm: %v\n", err)
	}

	instance, err := gceMakeInstance(opts, userdata, name)
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

// Create image on GCE and return. Will not overwrite existing image.
func GCECreateImage(api *compute.Service, proj, name, source string) error {
	image := &compute.Image{
		Name: name,
		RawDisk: &compute.ImageRawDisk{
			Source: source,
		},
	}
	_, err := api.Images.Insert(proj, image).Do()
	if err != nil {
		return err
	}
	return nil
}

// Delete image on GCE and then recreate it.
func GCEForceCreateImage(api *compute.Service, proj, name, source string) error {
	_, err := api.Images.Delete(proj, name).Do()

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
func gceMakeInstance(opts *GCEOptions, userdata string, name string) (*compute.Instance, error) {
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
	if userdata != "" {
		instance.Metadata.Items = append(instance.Metadata.Items, &compute.MetadataItems{
			Key:   "user-data",
			Value: userdata,
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
func nextName(api *compute.Service, opts *GCEOptions) (string, error) {
	base := opts.BaseName
	vms, err := GCEListVMs(api, opts, base)
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
	var err error

	// Allow a few authentication failures in case setup is slow.
	sshchecker := func() error {
		gm.sshClient, err = gm.gc.sshAgent.NewClient(gm.IP())
		if err != nil {
			return err
		}
		return nil
	}

	if err := util.Retry(sshRetries, sshTimeout, sshchecker); err != nil {
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
