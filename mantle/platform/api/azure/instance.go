// Copyright 2023 Red Hat
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
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"math"
	"math/big"
	"regexp"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork"

	"github.com/coreos/coreos-assembler/mantle/util"
)

type Machine struct {
	ID               string
	PublicIPAddress  string
	PrivateIPAddress string
	InterfaceName    string
	PublicIPName     string
}

func (a *API) getInstance(name, resourceGroup string) (armcompute.VirtualMachine, error) {
	resp, err := a.compClient.Get(context.Background(), resourceGroup, name, &armcompute.VirtualMachinesClientGetOptions{Expand: to.Ptr(armcompute.InstanceViewTypesInstanceView)})
	if err != nil {
		return armcompute.VirtualMachine{}, err
	}
	return resp.VirtualMachine, nil
}

func (a *API) getVMParameters(name, userdata, sshkey, storageAccountURI string, ip armnetwork.PublicIPAddress, nic armnetwork.Interface) armcompute.VirtualMachine {

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
	n, err := rand.Int(rand.Reader, big.NewInt(math.MaxInt64))
	if err != nil {
		panic(fmt.Sprintf("calling crypto/rand.Int() failed and that shouldn't happen: %v", err))
	}
	password := fmt.Sprintf("%s%s%s", "ABC&", n, "xyz")
	osProfile := armcompute.OSProfile{
		AdminUsername: to.Ptr("core"),   // unused
		AdminPassword: to.Ptr(password), // unused
		ComputerName:  &name,
	}
	if sshkey != "" {
		osProfile.LinuxConfiguration = &armcompute.LinuxConfiguration{
			SSH: &armcompute.SSHConfiguration{
				PublicKeys: []*armcompute.SSHPublicKey{
					{
						Path:    to.Ptr("/home/core/.ssh/authorized_keys"),
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
	var imgRef *armcompute.ImageReference
	if a.opts.DiskURI != "" {
		imgRef = &armcompute.ImageReference{
			ID: &a.opts.DiskURI,
		}
	} else {
		imgRef = &armcompute.ImageReference{
			Publisher: &a.opts.Publisher,
			Offer:     &a.opts.Offer,
			SKU:       &a.opts.Sku,
			Version:   &a.opts.Version,
		}
	}
	return armcompute.VirtualMachine{
		Name:     &name,
		Location: &a.opts.Location,
		Tags: map[string]*string{
			"createdBy": to.Ptr("mantle"),
		},
		Properties: &armcompute.VirtualMachineProperties{
			HardwareProfile: &armcompute.HardwareProfile{
				VMSize: to.Ptr(armcompute.VirtualMachineSizeTypes(a.opts.Size)),
			},
			StorageProfile: &armcompute.StorageProfile{
				ImageReference: imgRef,
				OSDisk: &armcompute.OSDisk{
					CreateOption: to.Ptr(armcompute.DiskCreateOptionTypesFromImage),
				},
			},
			OSProfile: &osProfile,
			NetworkProfile: &armcompute.NetworkProfile{
				NetworkInterfaces: []*armcompute.NetworkInterfaceReference{
					{
						ID: nic.ID,
						Properties: &armcompute.NetworkInterfaceReferenceProperties{
							Primary: to.Ptr(true),
						},
					},
				},
			},
			DiagnosticsProfile: &armcompute.DiagnosticsProfile{
				BootDiagnostics: &armcompute.BootDiagnostics{
					Enabled:    to.Ptr(true),
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	poller, err := a.compClient.BeginCreateOrUpdate(ctx, resourceGroup, name, vmParams, nil)
	if err != nil {
		return nil, fmt.Errorf("creating instance failed: %w", err)
	}
	_, err = poller.PollUntilDone(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("waiting on instance creation failed: %w", err)
	}

	err = util.WaitUntilReady(5*time.Minute, 10*time.Second, func() (bool, error) {
		resp, err := a.compClient.Get(context.Background(), resourceGroup, name, &armcompute.VirtualMachinesClientGetOptions{Expand: nil})
		if err != nil {
			return false, err
		}

		state := resp.VirtualMachine.Properties.ProvisioningState
		if state != nil && *state != "Succeeded" {
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

	vm, err := a.getInstance(name, resourceGroup)
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
	ctx := context.Background()
	poller, err := a.compClient.BeginDelete(ctx, resourceGroup, name, &armcompute.VirtualMachinesClientBeginDeleteOptions{ForceDeletion: to.Ptr(true)})
	if err != nil {
		return err
	}
	_, err = poller.PollUntilDone(ctx, nil)
	return err
}

func (a *API) GetConsoleOutput(name, resourceGroup, storageAccount string) ([]byte, error) {
	kr, err := a.GetStorageServiceKeys(storageAccount, resourceGroup)
	if err != nil {
		return nil, fmt.Errorf("retrieving storage service keys: %v", err)
	}

	if kr.Keys == nil {
		return nil, fmt.Errorf("no storage service keys found")
	}
	k := kr.Keys
	key := k[0].Value

	vm, err := a.getInstance(name, resourceGroup)
	if err != nil {
		return nil, err
	}

	consoleURI := vm.Properties.InstanceView.BootDiagnostics.SerialConsoleLogBlobURI
	if consoleURI == nil {
		return nil, fmt.Errorf("serial console URI is nil")
	}

	// Only the full URI to the logs are present in the virtual machine
	// properties. Parse out the container & file name to use the GetBlockBlob
	// API call directly.
	uri := []byte(*consoleURI)
	containerPat := regexp.MustCompile(`bootdiagnostics-kola[a-z0-9\-]+`)
	container := string(containerPat.Find(uri))
	namePat := regexp.MustCompile(`kola-[a-z0-9\-\.]+.serialconsole.log`)
	blobname := string(namePat.Find(uri))

	var data io.ReadCloser
	err = util.Retry(6, 10*time.Second, func() error {
		data, err = a.GetBlockBlob(storageAccount, *key, container, blobname)
		return err
	})
	if err != nil {
		return nil, err
	}

	return io.ReadAll(data)
}
