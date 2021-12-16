// Copyright 2018 CoreOS, Inc.
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

package azure

import (
	"encoding/base64"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"regexp"
	"strconv"
	"time"

	"github.com/Azure/azure-sdk-for-go/arm/compute"
	"github.com/Azure/azure-sdk-for-go/arm/network"

	"github.com/coreos/mantle/util"
)

type Machine struct {
	ID               string
	PublicIPAddress  string
	PrivateIPAddress string
	InterfaceName    string
	PublicIPName     string
}

func (a *API) getVMParameters(name, userdata, sshkey, storageAccountURI string, ip *network.PublicIPAddress, nic *network.Interface) compute.VirtualMachine {

	// Azure requires that either a username/password be set or an SSH key.
	//
	//    Message="Authentication using either SSH or by user name and
	//             password must be enabled in Linux profile."
	//
	// Since we don't ship their agent setting a username and password here
	// is harmless. We set the username and password because some tests explicitly
	// don't want to pass an SSH key via the API and this allows us to do that.
	//
	// The password requirements are:
	//    Message="The supplied password must be between 6-72 characters long
	//             and must satisfy at least 3 of password complexity requirements
	//             from the following: Contains an uppercase character, Contains a
	//             lowercase character, Contains a numeric digit, Contains a special
	//             character) Control characters are not allowed"
	password := fmt.Sprintf("%s%s%s", "ABC&", strconv.Itoa(rand.Int()), "xyz")

	osProfile := compute.OSProfile{
		AdminUsername: util.StrToPtr("core"),   // unused
		AdminPassword: util.StrToPtr(password), // unused
		ComputerName:  &name,
	}
	if sshkey != "" {
		osProfile.LinuxConfiguration = &compute.LinuxConfiguration{
			SSH: &compute.SSHConfiguration{
				PublicKeys: &[]compute.SSHPublicKey{
					{
						Path:    util.StrToPtr("/home/core/.ssh/authorized_keys"),
						KeyData: &sshkey,
					},
				},
			},
		}
	}
	if userdata != "" {
		ud := base64.StdEncoding.EncodeToString([]byte(userdata))
		osProfile.CustomData = &ud
	}
	var imgRef *compute.ImageReference
	if a.opts.DiskURI != "" {
		imgRef = &compute.ImageReference{
			ID: &a.opts.DiskURI,
		}
	} else {
		imgRef = &compute.ImageReference{
			Publisher: &a.opts.Publisher,
			Offer:     &a.opts.Offer,
			Sku:       &a.opts.Sku,
			Version:   &a.opts.Version,
		}
	}
	return compute.VirtualMachine{
		Name:     &name,
		Location: &a.opts.Location,
		Tags: &map[string]*string{
			"createdBy": util.StrToPtr("mantle"),
		},
		VirtualMachineProperties: &compute.VirtualMachineProperties{
			HardwareProfile: &compute.HardwareProfile{
				VMSize: compute.VirtualMachineSizeTypes(a.opts.Size),
			},
			StorageProfile: &compute.StorageProfile{
				ImageReference: imgRef,
				OsDisk: &compute.OSDisk{
					CreateOption: compute.FromImage,
				},
			},
			OsProfile: &osProfile,
			NetworkProfile: &compute.NetworkProfile{
				NetworkInterfaces: &[]compute.NetworkInterfaceReference{
					{
						ID: nic.ID,
						NetworkInterfaceReferenceProperties: &compute.NetworkInterfaceReferenceProperties{
							Primary: util.BoolToPtr(true),
						},
					},
				},
			},
			DiagnosticsProfile: &compute.DiagnosticsProfile{
				BootDiagnostics: &compute.BootDiagnostics{
					Enabled:    util.BoolToPtr(true),
					StorageURI: &storageAccountURI,
				},
			},
		},
	}
}

func (a *API) CreateInstance(name, userdata, sshkey, resourceGroup, storageAccount string) (*Machine, error) {
	subnet, err := a.getSubnet(resourceGroup)
	if err != nil {
		return nil, fmt.Errorf("preparing network resources: %v", err)
	}

	ip, err := a.createPublicIP(resourceGroup)
	if err != nil {
		return nil, fmt.Errorf("creating public ip: %v", err)
	}
	if ip.Name == nil {
		return nil, fmt.Errorf("couldn't get public IP name")
	}

	nic, err := a.createNIC(ip, &subnet, resourceGroup)
	if err != nil {
		return nil, fmt.Errorf("creating nic: %v", err)
	}
	if nic.Name == nil {
		return nil, fmt.Errorf("couldn't get NIC name")
	}

	vmParams := a.getVMParameters(name, userdata, sshkey, fmt.Sprintf("https://%s.blob.core.windows.net/", storageAccount), ip, nic)

	_, err = a.compClient.CreateOrUpdate(resourceGroup, name, vmParams, nil)
	if err != nil {
		return nil, err
	}

	err = util.WaitUntilReady(5*time.Minute, 10*time.Second, func() (bool, error) {
		vm, err := a.compClient.Get(resourceGroup, name, "")
		if err != nil {
			return false, err
		}

		if vm.VirtualMachineProperties.ProvisioningState != nil && *vm.VirtualMachineProperties.ProvisioningState != "Succeeded" {
			return false, nil
		}

		return true, nil
	})
	if err != nil {
		if errTerminate := a.TerminateInstance(name, resourceGroup); errTerminate != nil {
			return nil, fmt.Errorf("terminating machines failed: %v after the machines failed to become active: %v", errTerminate, err)
		}
		return nil, fmt.Errorf("waiting for machine to become active: %v", err)
	}

	vm, err := a.compClient.Get(resourceGroup, name, "")
	if err != nil {
		return nil, err
	}

	if vm.Name == nil {
		return nil, fmt.Errorf("couldn't get VM ID")
	}

	publicaddr, privaddr, err := a.GetIPAddresses(*nic.Name, *ip.Name, resourceGroup)
	if err != nil {
		return nil, err
	}

	return &Machine{
		ID:               *vm.Name,
		PublicIPAddress:  publicaddr,
		PrivateIPAddress: privaddr,
		InterfaceName:    *nic.Name,
		PublicIPName:     *ip.Name,
	}, nil
}

func (a *API) TerminateInstance(name, resourceGroup string) error {
	_, err := a.compClient.Delete(resourceGroup, name, nil)
	return err
}

func (a *API) GetConsoleOutput(name, resourceGroup, storageAccount string) ([]byte, error) {
	kr, err := a.GetStorageServiceKeysARM(storageAccount, resourceGroup)
	if err != nil {
		return nil, fmt.Errorf("retrieving storage service keys: %v", err)
	}

	if kr.Keys == nil {
		return nil, fmt.Errorf("no storage service keys found")
	}
	k := *kr.Keys
	key := *k[0].Value

	vm, err := a.compClient.Get(resourceGroup, name, compute.InstanceView)
	if err != nil {
		return nil, err
	}

	consoleURI := vm.VirtualMachineProperties.InstanceView.BootDiagnostics.SerialConsoleLogBlobURI
	if consoleURI == nil {
		return nil, fmt.Errorf("serial console URI is nil")
	}

	// Only the full URI to the logs are present in the virtual machine
	// properties. Parse out the container & file name to use the GetBlob
	// API call directly.
	uri := []byte(*consoleURI)
	containerPat := regexp.MustCompile(`bootdiagnostics-kola[a-z0-9\-]+`)
	container := string(containerPat.Find(uri))
	namePat := regexp.MustCompile(`kola-[a-z0-9\-\.]+.serialconsole.log`)
	blobname := string(namePat.Find(uri))

	var data io.ReadCloser
	err = util.Retry(6, 10*time.Second, func() error {
		data, err = a.GetBlob(storageAccount, key, container, blobname)
		return err
	})
	if err != nil {
		return nil, err
	}

	return ioutil.ReadAll(data)
}
