// Copyright (c) 2016 VMware, Inc. All Rights Reserved.
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

package esx

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/coreos/pkg/capnslog"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/ovf"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/progress"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"

	"github.com/coreos/coreos-assembler/mantle/auth"
	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
)

type Options struct {
	*platform.Options

	// Config file. Defaults to $HOME/.config/esx.json
	ConfigPath string
	// Profile name
	Profile string

	Server     string
	User       string
	Password   string
	BaseVMName string
}

var plog = capnslog.NewPackageLogger("github.com/coreos/coreos-assembler/mantle", "platform/api/esx")

type API struct {
	options *Options
	client  *govmomi.Client
	ctx     context.Context
}

type ESXMachine struct {
	Name      string
	IPAddress string
}

type ovfFileItem struct {
	url  *url.URL
	item types.OvfFileItem
	ch   chan progress.Report
}

type serverResources struct {
	finder       *find.Finder
	datacenter   *object.Datacenter
	resourcePool *object.ResourcePool
	datastore    *object.Datastore
	network      object.NetworkReference
}

func (a *API) getMachine(vm *object.VirtualMachine) (*ESXMachine, error) {
	ctx := context.Background()
	deadline, cancel := context.WithDeadline(ctx, time.Now().Add(1000*time.Second))
	defer cancel()
	ip, err := vm.WaitForNetIP(deadline, false)
	if err != nil {
		return nil, fmt.Errorf("waiting for net ip: %v", err)
	}

	var ipaddr string
OUTER:
	for _, ips := range ip {
		for _, val := range ips {
			addr := net.ParseIP(val)
			if addr.IsLinkLocalMulticast() || addr.IsLinkLocalUnicast() {
				continue
			}
			ipaddr = val
			break OUTER
		}
	}

	var mvm mo.VirtualMachine
	err = vm.Properties(ctx, vm.Reference(), []string{"summary"}, &mvm)
	if err != nil {
		return nil, fmt.Errorf("getting machine reference: %v", err)
	}

	return &ESXMachine{
		Name:      mvm.Summary.Config.Name,
		IPAddress: ipaddr,
	}, nil
}

func New(opts *Options) (*API, error) {
	if opts.Server == "" || opts.User == "" || opts.Password == "" {
		profiles, err := auth.ReadESXConfig(opts.ConfigPath)
		if err != nil {
			return nil, fmt.Errorf("couldn't read ESX config: %v", err)
		}

		if opts.Profile == "" {
			opts.Profile = "default"
		}
		profile, ok := profiles[opts.Profile]
		if !ok {
			return nil, fmt.Errorf("no such profile %q", opts.Profile)
		}
		if opts.Server == "" {
			opts.Server = profile.Server
		}
		if opts.User == "" {
			opts.User = profile.User
		}
		if opts.Password == "" {
			opts.Password = profile.Password
		}
	}

	esxUrl := fmt.Sprintf("%s:%s@%s", opts.User, opts.Password, opts.Server)
	u, err := soap.ParseURL(esxUrl)
	if err != nil {
		return nil, fmt.Errorf("parsing ESX URL: %v", err)
	}

	ctx := context.Background()

	client, err := govmomi.NewClient(ctx, u, true)
	if err != nil {
		return nil, fmt.Errorf("connecting to ESX: %v", err)
	}

	return &API{
		options: opts,
		client:  client,
		ctx:     ctx,
	}, nil
}

func getNetworkDevice(net object.NetworkReference) (types.BaseVirtualDevice, error) {
	backing, err := net.EthernetCardBackingInfo(context.TODO())
	if err != nil {
		return nil, err
	}

	device, err := object.EthernetCardTypes().CreateEthernetCard("e1000", backing)
	if err != nil {
		return nil, err
	}

	return device, nil
}

func (a *API) buildCloneSpec(baseVM *object.VirtualMachine, folder *object.Folder, network object.NetworkReference, resourcePool *object.ResourcePool, datastore *object.Datastore, userdata string) (*types.VirtualMachineCloneSpec, error) {
	devices, err := baseVM.Device(a.ctx)
	if err != nil {
		return nil, fmt.Errorf("couldn't get base VM devices: %v", err)
	}
	var card *types.VirtualEthernetCard
	for _, device := range devices {
		if c, ok := device.(types.BaseVirtualEthernetCard); ok {
			card = c.GetVirtualEthernetCard()
			break
		}
	}
	if card == nil {
		return nil, fmt.Errorf("No network device found.")
	}

	netDev, err := getNetworkDevice(network)
	if err != nil {
		return nil, fmt.Errorf("couldn't get new network backing device: %v", err)
	}

	card.Backing = netDev.(types.BaseVirtualEthernetCard).GetVirtualEthernetCard().Backing

	folderRef := folder.Reference()
	poolRef := resourcePool.Reference()
	datastoreRef := datastore.Reference()

	cloneSpec := &types.VirtualMachineCloneSpec{
		Location: types.VirtualMachineRelocateSpec{
			DeviceChange: []types.BaseVirtualDeviceConfigSpec{
				&types.VirtualDeviceConfigSpec{
					Operation: types.VirtualDeviceConfigSpecOperationEdit,
					Device:    card,
				},
			},
			Folder:    &folderRef,
			Pool:      &poolRef,
			Datastore: &datastoreRef,
		},
		PowerOn:  false,
		Template: false,
	}

	return cloneSpec, nil
}

func (a *API) addSerialPort(vm *object.VirtualMachine) error {
	devices, err := vm.Device(a.ctx)
	if err != nil {
		return fmt.Errorf("couldn't get devices for vm: %v", err)
	}

	d, err := devices.CreateSerialPort()
	if err != nil {
		return fmt.Errorf("couldn't create serial port: %v", err)
	}

	err = vm.AddDevice(a.ctx, d)
	if err != nil {
		return fmt.Errorf("couldn't add serial port to vm: %v", err)
	}

	// refresh devices
	devices, err = vm.Device(a.ctx)
	if err != nil {
		return fmt.Errorf("couldn't get devices for vm: %v", err)
	}

	d, err = devices.FindSerialPort("")
	if err != nil {
		return fmt.Errorf("couldn't find serial port for vm: %v", err)
	}

	var mvm mo.VirtualMachine
	err = vm.Properties(a.ctx, vm.Reference(), []string{"config.files.logDirectory"}, &mvm)
	if err != nil {
		return fmt.Errorf("couldn't get log directory: %v", err)
	}
	uri := path.Join(mvm.Config.Files.LogDirectory, "serial.out")

	return vm.EditDevice(a.ctx, devices.ConnectSerialPort(d, uri, false, ""))
}

func (a *API) GetConsoleOutput(name string) (string, error) {
	defaults, err := a.getServerDefaults()
	if err != nil {
		return "", fmt.Errorf("couldn't get server defaults: %v", err)
	}

	uri := fmt.Sprintf("%s/serial.out", name)

	p := soap.DefaultDownload

	f, _, err := defaults.datastore.Download(a.ctx, uri, &p)
	if err != nil {
		return "", fmt.Errorf("couldn't download console logs: %v", err)
	}
	defer f.Close()

	buf, err := io.ReadAll(f)
	if err != nil {
		return "", fmt.Errorf("couldn't read serial output: %v", err)
	}

	return string(buf), nil
}

func (a *API) CleanupDevice(name string) error {
	defaults, err := a.getServerDefaults()
	if err != nil {
		return fmt.Errorf("couldn't get server defaults: %v", err)
	}

	_, err = defaults.finder.VirtualMachine(a.ctx, name)
	if err == nil {
		return fmt.Errorf("VM still exists")
	}

	fm := defaults.datastore.NewFileManager(defaults.datacenter, true)

	// Remove the serial.out file
	uri := fmt.Sprintf("%s/serial.out", name)
	err = fm.DeleteFile(a.ctx, uri)
	if err != nil && !strings.HasSuffix(err.Error(), "was not found") {
		return fmt.Errorf("couldn't delete serial.out: %v", err)
	}

	// Remove the VM directory
	err = fm.DeleteFile(a.ctx, name)
	if err != nil && !strings.HasSuffix(err.Error(), "was not found") {
		return fmt.Errorf("couldn't delete vm directory: %v", err)
	}

	return nil
}

func (a *API) CreateDevice(name string, conf *conf.Conf) (*ESXMachine, error) {
	if a.options.BaseVMName == "" {
		return nil, fmt.Errorf("Base VM Name must be supplied")
	}

	userdata := base64.StdEncoding.EncodeToString(conf.Bytes())

	defaults, err := a.getServerDefaults()
	if err != nil {
		return nil, fmt.Errorf("couldn't get server defaults: %v", err)
	}

	baseVM, err := defaults.finder.VirtualMachine(a.ctx, a.options.BaseVMName)
	if err != nil {
		return nil, fmt.Errorf("couldn't find base VM: %v", err)
	}

	folders, err := defaults.datacenter.Folders(a.ctx)
	if err != nil {
		return nil, fmt.Errorf("getting datacenter folders: %v", err)
	}
	folder := folders.VmFolder

	cloneSpec, err := a.buildCloneSpec(baseVM, folder, defaults.network, defaults.resourcePool, defaults.datastore, userdata)
	if err != nil {
		return nil, fmt.Errorf("failed building clone spec: %v", err)
	}

	task, err := baseVM.Clone(a.ctx, folder, name, *cloneSpec)
	if err != nil {
		return nil, fmt.Errorf("couldn't clone base VM: %v", err)
	}

	err = task.Wait(a.ctx)
	if err != nil {
		return nil, fmt.Errorf("clone base VM operation failed: %v", err)
	}

	vm, err := defaults.finder.VirtualMachine(a.ctx, name)
	if err != nil {
		return nil, fmt.Errorf("couldn't find cloned VM: %v", err)
	}

	err = a.addSerialPort(vm)
	if err != nil {
		return nil, fmt.Errorf("adding serial port: %v", err)
	}

	err = a.updateOVFEnv(vm, userdata)
	if err != nil {
		return nil, fmt.Errorf("setting guestinfo settings: %v", err)
	}

	err = a.startVM(vm)
	if err != nil {
		return nil, fmt.Errorf("starting vm: %v", err)
	}

	mach, err := a.getMachine(vm)
	if err != nil {
		return nil, fmt.Errorf("getting machine info: %v", err)
	}

	return mach, nil
}

func (a *API) CreateBaseDevice(name, ovaPath string) error {
	if ovaPath == "" {
		return fmt.Errorf("ova path cannot be empty")
	}

	defaults, err := a.getServerDefaults()
	if err != nil {
		return fmt.Errorf("getting ESX defaults: %v", err)
	}

	arch, cisr, err := a.buildCreateImportSpecRequest(name, ovaPath, defaults.finder, defaults.resourcePool, defaults.datastore)
	if err != nil {
		return fmt.Errorf("building CreateImportSpecRequest: %v", err)
	}

	folders, err := defaults.datacenter.Folders(a.ctx)
	if err != nil {
		return fmt.Errorf("getting datacenter folders: %v", err)
	}
	folder := folders.VmFolder

	entity, err := a.uploadToResourcePool(arch, defaults.resourcePool, cisr, folder)
	if err != nil {
		return fmt.Errorf("uploading disks to ResourcePool: %v", err)
	}

	// object.NewVirtualMachine returns a VirtualMachine object but we don't
	// need to do anything with the returned object so ignore it
	_ = object.NewVirtualMachine(a.client.Client, *entity)

	return nil
}

func (a *API) TerminateDevice(name string) error {
	defaults, err := a.getServerDefaults()
	if err != nil {
		return fmt.Errorf("couldn't get server defaults: %v", err)
	}

	vm, err := defaults.finder.VirtualMachine(a.ctx, name)
	if err != nil {
		return fmt.Errorf("couldn't find VM: %v", err)
	}

	return a.deleteDevice(vm)
}

func (a *API) deleteDevice(vm *object.VirtualMachine) error {
	task, err := vm.PowerOff(a.ctx)
	if err != nil {
		return fmt.Errorf("powering off vm: %v", err)
	}

	// We don't check for errors on this task because it will throw an error
	// if the VM is already in a powered off state
	_ = task.Wait(a.ctx)

	task, err = vm.Destroy(a.ctx)
	if err != nil {
		return fmt.Errorf("destroying vm: %v", vm)
	}

	return task.Wait(a.ctx)
}

func (a *API) buildCreateImportSpecRequest(name string, ovaPath string, finder *find.Finder, resourcePool *object.ResourcePool, datastore *object.Datastore) (*archive, *types.OvfCreateImportSpecResult, error) {
	arch := &archive{ovaPath}
	envelope, err := arch.readEnvelope("*.ovf")
	if err != nil {
		return nil, nil, fmt.Errorf("reading envelope: %v", err)
	}

	ovfHandler := object.NewOvfManager(a.client.Client)
	cisp := types.OvfCreateImportSpecParams{
		EntityName: name,
		OvfManagerCommonParams: types.OvfManagerCommonParams{
			Locale: "US"},
		PropertyMapping: []types.KeyValue{},
		NetworkMapping:  networkMap(finder, envelope),
	}

	descriptor, err := arch.readOvf("*.ovf")
	if err != nil {
		return nil, nil, fmt.Errorf("reading ovf: %v", err)
	}
	cisr, err := ovfHandler.CreateImportSpec(a.ctx, string(descriptor), resourcePool, datastore, cisp)
	if err != nil {
		return nil, nil, err
	}
	if cisr.Error != nil {
		return nil, nil, errors.New(cisr.Error[0].LocalizedMessage)
	}
	return arch, cisr, nil
}

func (a *API) getServerDefaults() (serverResources, error) {
	finder := find.NewFinder(a.client.Client, true)
	datacenter, err := finder.DefaultDatacenter(a.ctx)
	if err != nil {
		return serverResources{}, err
	}
	finder.SetDatacenter(datacenter)
	resourcePool, err := finder.DefaultResourcePool(a.ctx)
	if err != nil {
		return serverResources{}, err
	}
	datastore, err := finder.DefaultDatastore(a.ctx)
	if err != nil {
		return serverResources{}, err
	}

	defaultNetwork, err := finder.DefaultNetwork(a.ctx)
	if err != nil {
		return serverResources{}, err
	}

	return serverResources{
		finder:       finder,
		datacenter:   datacenter,
		resourcePool: resourcePool,
		datastore:    datastore,
		network:      defaultNetwork,
	}, nil
}

func (a *API) uploadToResourcePool(arch *archive, resourcePool *object.ResourcePool, cisr *types.OvfCreateImportSpecResult, folder *object.Folder) (*types.ManagedObjectReference, error) {
	lease, err := resourcePool.ImportVApp(a.ctx, cisr.ImportSpec, folder, nil)
	if err != nil {
		return nil, fmt.Errorf("importing vApp: %v", err)
	}

	info, err := lease.Wait(a.ctx)
	if err != nil {
		return nil, err
	}

	var items []ovfFileItem

	for _, device := range info.DeviceUrl {
		for _, item := range cisr.FileItem {
			if device.ImportKey != item.DeviceId {
				continue
			}

			u, err := a.client.Client.ParseURL(device.Url)
			if err != nil {
				return nil, err
			}

			i := ovfFileItem{
				url:  u,
				item: item,
				ch:   make(chan progress.Report),
			}

			items = append(items, i)
		}
	}

	upd := newLeaseUpdater(a.client.Client, lease, items)
	defer upd.Done()

	for _, i := range items {
		err = a.upload(arch, lease, i)
		if err != nil {
			return nil, err
		}
	}

	err = lease.HttpNfcLeaseComplete(a.ctx)
	if err != nil {
		return nil, err
	}
	return &info.Entity, nil
}

func networkMap(finder *find.Finder, e *ovf.Envelope) (p []types.OvfNetworkMapping) {
	if e.Network != nil {
		for _, net := range e.Network.Networks {
			if n, err := finder.Network(context.TODO(), net.Name); err == nil {
				p = append(p, types.OvfNetworkMapping{
					Name:    net.Name,
					Network: n.Reference(),
				})
			}
		}
	}
	return
}

func (a *API) updateOVFEnv(vm *object.VirtualMachine, userdata string) error {
	var property []types.VAppPropertySpec

	var mvm mo.VirtualMachine
	err := vm.Properties(a.ctx, vm.Reference(), []string{"config", "config.vAppConfig", "config.vAppConfig.property"}, &mvm)
	if err != nil {
		return fmt.Errorf("couldn't get config.vappconfig: %v", err)
	}

	for _, item := range mvm.Config.VAppConfig.(*types.VmConfigInfo).Property {
		if item.Id == "guestinfo.coreos.config.data" {
			property = append(property, types.VAppPropertySpec{
				ArrayUpdateSpec: types.ArrayUpdateSpec{
					Operation: types.ArrayUpdateOperationEdit,
				},
				Info: &types.VAppPropertyInfo{
					Key:          item.Key,
					Id:           item.Id,
					DefaultValue: userdata,
				},
			})
		} else if item.Id == "guestinfo.coreos.config.data.encoding" {
			property = append(property, types.VAppPropertySpec{
				ArrayUpdateSpec: types.ArrayUpdateSpec{
					Operation: types.ArrayUpdateOperationEdit,
				},
				Info: &types.VAppPropertyInfo{
					Key:          item.Key,
					Id:           item.Id,
					DefaultValue: "base64",
				},
			})
		}
	}

	if len(property) != 2 {
		return fmt.Errorf("couldn't find required vApp properties on vm")
	}

	task, err := vm.Reconfigure(a.ctx, types.VirtualMachineConfigSpec{
		VAppConfig: &types.VmConfigSpec{
			Property: property,
		},
	})

	if err != nil {
		return err
	}

	return task.Wait(a.ctx)
}

func (a *API) startVM(vm *object.VirtualMachine) error {
	task, err := vm.PowerOn(a.ctx)
	if err != nil {
		return err
	}

	return task.Wait(a.ctx)
}

func (a *API) upload(arch *archive, lease *object.HttpNfcLease, ofi ovfFileItem) error {
	item := ofi.item
	file := item.Path

	f, size, err := arch.open(file)
	if err != nil {
		return err
	}
	defer f.Close()

	opts := soap.Upload{
		ContentLength: size,
		Progress:      nil,
	}

	// Non-disk files (such as .iso) use the PUT method.
	// Overwrite: t header is also required in this case (ovftool does the same)
	if item.Create {
		opts.Method = "PUT"
		opts.Headers = map[string]string{
			"Overwrite": "t",
		}
	} else {
		opts.Method = "POST"
		opts.Type = "application/x-vnd.vmware-streamVmdk"
	}

	return a.client.Client.Upload(f, ofi.url, &opts)
}

func (a *API) PreflightCheck() error {
	var mgr mo.SessionManager

	c := a.client.Client

	return mo.RetrieveProperties(context.Background(), c, c.ServiceContent.PropertyCollector, *c.ServiceContent.SessionManager, &mgr)
}
