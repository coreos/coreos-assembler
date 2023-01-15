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
	"fmt"

	"github.com/Azure/azure-sdk-for-go/arm/network"

	"github.com/coreos/coreos-assembler/mantle/util"
)

var (
	virtualNetworkPrefix = []string{"10.0.0.0/16"}
	subnetPrefix         = "10.0.0.0/24"
)

func (a *API) PrepareNetworkResources(resourceGroup string) (network.Subnet, error) {
	if err := a.createVirtualNetwork(resourceGroup); err != nil {
		return network.Subnet{}, err
	}

	return a.createSubnet(resourceGroup)
}

func (a *API) createVirtualNetwork(resourceGroup string) error {
	_, err := a.netClient.CreateOrUpdate(resourceGroup, "kola-vn", network.VirtualNetwork{
		Location: &a.opts.Location,
		VirtualNetworkPropertiesFormat: &network.VirtualNetworkPropertiesFormat{
			AddressSpace: &network.AddressSpace{
				AddressPrefixes: &virtualNetworkPrefix,
			},
		},
	}, nil)

	return err
}

func (a *API) createSubnet(resourceGroup string) (network.Subnet, error) {
	_, err := a.subClient.CreateOrUpdate(resourceGroup, "kola-vn", "kola-subnet", network.Subnet{
		SubnetPropertiesFormat: &network.SubnetPropertiesFormat{
			AddressPrefix: &subnetPrefix,
		},
	}, nil)
	if err != nil {
		return network.Subnet{}, err
	}

	return a.getSubnet(resourceGroup)
}

func (a *API) getSubnet(resourceGroup string) (network.Subnet, error) {
	return a.subClient.Get(resourceGroup, "kola-vn", "kola-subnet", "")
}

func (a *API) createPublicIP(resourceGroup string) (*network.PublicIPAddress, error) {
	name := randomName("ip")

	_, err := a.ipClient.CreateOrUpdate(resourceGroup, name, network.PublicIPAddress{
		Location: &a.opts.Location,
	}, nil)
	if err != nil {
		return nil, err
	}

	ip, err := a.ipClient.Get(resourceGroup, name, "")
	if err != nil {
		return nil, err
	}

	return &ip, nil
}

func (a *API) GetPublicIP(name, resourceGroup string) (string, error) {
	ip, err := a.ipClient.Get(resourceGroup, name, "")
	if err != nil {
		return "", err
	}

	if ip.PublicIPAddressPropertiesFormat.IPAddress == nil {
		return "", fmt.Errorf("IP Address is nil")
	}

	return *ip.PublicIPAddressPropertiesFormat.IPAddress, nil
}

// returns PublicIP, PrivateIP, error
func (a *API) GetIPAddresses(name, publicIPName, resourceGroup string) (string, string, error) {
	publicIP, err := a.GetPublicIP(publicIPName, resourceGroup)
	if err != nil {
		return "", "", err
	}

	nic, err := a.intClient.Get(resourceGroup, name, "")
	if err != nil {
		return "", "", err
	}

	configs := *nic.InterfacePropertiesFormat.IPConfigurations

	for _, conf := range configs {
		if conf.PrivateIPAddress == nil {
			return "", "", fmt.Errorf("PrivateIPAddress is nil")
		} else {
			return publicIP, *conf.PrivateIPAddress, nil
		}
	}
	return "", "", fmt.Errorf("no ip configurations found")
}

func (a *API) GetPrivateIP(name, resourceGroup string) (string, error) {
	nic, err := a.intClient.Get(resourceGroup, name, "")
	if err != nil {
		return "", err
	}

	configs := *nic.InterfacePropertiesFormat.IPConfigurations
	return *configs[0].PrivateIPAddress, nil
}

func (a *API) createNIC(ip *network.PublicIPAddress, subnet *network.Subnet, resourceGroup string) (*network.Interface, error) {
	name := randomName("nic")
	ipconf := randomName("nic-ipconf")

	_, err := a.intClient.CreateOrUpdate(resourceGroup, name, network.Interface{
		Location: &a.opts.Location,
		InterfacePropertiesFormat: &network.InterfacePropertiesFormat{
			IPConfigurations: &[]network.InterfaceIPConfiguration{
				{
					Name: &ipconf,
					InterfaceIPConfigurationPropertiesFormat: &network.InterfaceIPConfigurationPropertiesFormat{
						PublicIPAddress:           ip,
						PrivateIPAllocationMethod: network.Dynamic,
						Subnet:                    subnet,
					},
				},
			},
			EnableAcceleratedNetworking: util.BoolToPtr(true),
		},
	}, nil)
	if err != nil {
		return nil, err
	}

	nic, err := a.intClient.Get(resourceGroup, name, "")
	if err != nil {
		return nil, err
	}

	return &nic, nil
}
