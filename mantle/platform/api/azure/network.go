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
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork"

	"github.com/coreos/coreos-assembler/mantle/util"
)

var (
	virtualNetworkPrefix = "10.0.0.0/16"
	subnetPrefix         = "10.0.0.0/24"
)

func (a *API) PrepareNetworkResources(resourceGroup string) (armnetwork.Subnet, error) {
	if err := a.createVirtualNetwork(resourceGroup); err != nil {
		return armnetwork.Subnet{}, err
	}

	return a.createSubnet(resourceGroup)
}

func (a *API) createVirtualNetwork(resourceGroup string) error {
	ctx := context.Background()
	poller, err := a.netClient.BeginCreateOrUpdate(ctx, resourceGroup, "kola-vn", armnetwork.VirtualNetwork{
		Location: to.Ptr(a.opts.Location),
		Properties: &armnetwork.VirtualNetworkPropertiesFormat{
			AddressSpace: &armnetwork.AddressSpace{
				AddressPrefixes: []*string{to.Ptr(virtualNetworkPrefix)},
			},
		},
	}, nil)
	if err != nil {
		return err
	}
	_, err = poller.PollUntilDone(ctx, nil)
	return err
}

func (a *API) createSubnet(resourceGroup string) (armnetwork.Subnet, error) {
	ctx := context.Background()
	poller, err := a.subClient.BeginCreateOrUpdate(ctx, resourceGroup, "kola-vn", "kola-subnet", armnetwork.Subnet{
		Properties: &armnetwork.SubnetPropertiesFormat{
			AddressPrefix: to.Ptr(subnetPrefix),
		},
	}, nil)
	if err != nil {
		return armnetwork.Subnet{}, err
	}
	resp, err := poller.PollUntilDone(ctx, nil)
	if err != nil {
		return armnetwork.Subnet{}, err
	}
	return resp.Subnet, nil
}

func (a *API) getSubnet(resourceGroup string) (armnetwork.Subnet, error) {
	resp, err := a.subClient.Get(context.Background(), resourceGroup, "kola-vn", "kola-subnet", &armnetwork.SubnetsClientGetOptions{Expand: nil})
	if err != nil {
		return armnetwork.Subnet{}, err
	}
	return resp.Subnet, nil
}

func (a *API) createPublicIP(resourceGroup string) (armnetwork.PublicIPAddress, error) {
	name := util.RandomName("ip")
	ctx := context.Background()

	var ipSKU *armnetwork.PublicIPAddressSKU
	var ipProperties *armnetwork.PublicIPAddressPropertiesFormat
	var ipZones []*string

	// set SKU=Standard, Allocation Method=Static and Availability Zone on public IPs when creating Gen2 images
	if strings.EqualFold(a.opts.HyperVGeneration, string(armcompute.HyperVGenerationV2)) {
		ipSKU = &armnetwork.PublicIPAddressSKU{
			Name: to.Ptr(armnetwork.PublicIPAddressSKUNameStandard),
		}
		ipProperties = &armnetwork.PublicIPAddressPropertiesFormat{
			PublicIPAllocationMethod: to.Ptr(armnetwork.IPAllocationMethodStatic),
		}
		ipZones = []*string{to.Ptr(a.opts.AvailabilityZone)}
		// gen 1
	} else {
		ipSKU = &armnetwork.PublicIPAddressSKU{
			Name: to.Ptr(armnetwork.PublicIPAddressSKUNameBasic),
		}
		ipProperties = &armnetwork.PublicIPAddressPropertiesFormat{
			PublicIPAllocationMethod: to.Ptr(armnetwork.IPAllocationMethodDynamic),
		}
		// No Zones for Gen1
		ipZones = nil
	}

	poller, err := a.ipClient.BeginCreateOrUpdate(ctx, resourceGroup, name, armnetwork.PublicIPAddress{
		Location:   to.Ptr(a.opts.Location),
		Zones:      ipZones,
		SKU:        ipSKU,
		Properties: ipProperties,
	}, nil)
	if err != nil {
		return armnetwork.PublicIPAddress{}, err
	}

	resp, err := poller.PollUntilDone(ctx, nil)
	if err != nil {
		return armnetwork.PublicIPAddress{}, err
	}

	return resp.PublicIPAddress, nil
}

func (a *API) GetPublicIP(name, resourceGroup string) (string, error) {
	resp, err := a.ipClient.Get(context.Background(), resourceGroup, name, &armnetwork.PublicIPAddressesClientGetOptions{Expand: nil})
	if err != nil {
		return "", err
	}

	ip := resp.PublicIPAddress
	if ip.Properties.IPAddress == nil {
		return "", fmt.Errorf("IP Address is nil")
	}

	return *ip.Properties.IPAddress, nil
}

// returns PublicIP, PrivateIP, error
func (a *API) GetIPAddresses(name, publicIPName, resourceGroup string) (string, string, error) {
	publicIP, err := a.GetPublicIP(publicIPName, resourceGroup)
	if err != nil {
		return "", "", err
	}
	privateIP, err := a.GetPrivateIP(name, resourceGroup)
	if err != nil {
		return publicIP, "", err
	}
	return publicIP, privateIP, nil
}

func (a *API) GetPrivateIP(interfaceName, resourceGroup string) (string, error) {
	resp, err := a.intClient.Get(context.Background(), resourceGroup, interfaceName, &armnetwork.InterfacesClientGetOptions{Expand: nil})
	if err != nil {
		return "", err
	}
	nic := resp.Interface

	configs := nic.Properties.IPConfigurations

	for _, conf := range configs {
		if conf.Properties.PrivateIPAddress == nil {
			return "", fmt.Errorf("PrivateIPAddress is nil")
		} else {
			return *conf.Properties.PrivateIPAddress, nil
		}
	}
	return "", fmt.Errorf("no private configurations found")
}

func (a *API) createNIC(ip armnetwork.PublicIPAddress, subnet *armnetwork.Subnet, nsg *armnetwork.SecurityGroup, resourceGroup string) (armnetwork.Interface, error) {
	name := util.RandomName("nic")
	ipconf := util.RandomName("nic-ipconf")
	ctx := context.Background()

	poller, err := a.intClient.BeginCreateOrUpdate(ctx, resourceGroup, name, armnetwork.Interface{
		Location: to.Ptr(a.opts.Location),
		Properties: &armnetwork.InterfacePropertiesFormat{
			NetworkSecurityGroup: nsg,
			IPConfigurations: []*armnetwork.InterfaceIPConfiguration{
				{
					Name: to.Ptr(ipconf),
					Properties: &armnetwork.InterfaceIPConfigurationPropertiesFormat{
						PublicIPAddress:           to.Ptr(ip),
						PrivateIPAllocationMethod: to.Ptr(armnetwork.IPAllocationMethodDynamic),
						Subnet:                    subnet,
					},
				},
			},
			EnableAcceleratedNetworking: to.Ptr(true),
		},
	}, nil)
	if err != nil {
		return armnetwork.Interface{}, err
	}

	resp, err := poller.PollUntilDone(ctx, nil)
	if err != nil {
		return armnetwork.Interface{}, err
	}
	nic := resp.Interface

	return nic, nil
}

func (a *API) CreateNSG(resourceGroup string) (armnetwork.SecurityGroup, error) {
	name := util.RandomName("nsg")
	ctx := context.Background()

	sshRule := &armnetwork.SecurityRule{
		Name: to.Ptr("allow_ssh"),
		Properties: &armnetwork.SecurityRulePropertiesFormat{
			Access:                   to.Ptr(armnetwork.SecurityRuleAccessAllow),
			Direction:                to.Ptr(armnetwork.SecurityRuleDirectionInbound),
			Protocol:                 to.Ptr(armnetwork.SecurityRuleProtocolTCP),
			SourcePortRange:          to.Ptr("*"),
			DestinationPortRange:     to.Ptr("22"),
			SourceAddressPrefix:      to.Ptr("*"),
			DestinationAddressPrefix: to.Ptr("*"),
			Priority:                 to.Ptr(int32(1000)),
		},
	}

	nsgParams := armnetwork.SecurityGroup{
		Location: to.Ptr(a.opts.Location),
		Properties: &armnetwork.SecurityGroupPropertiesFormat{
			SecurityRules: []*armnetwork.SecurityRule{sshRule},
		},
	}

	poller, err := a.nsgClient.BeginCreateOrUpdate(ctx, resourceGroup, name, nsgParams, nil)
	if err != nil {
		return armnetwork.SecurityGroup{}, err
	}

	resp, err := poller.PollUntilDone(ctx, nil)
	if err != nil {
		return armnetwork.SecurityGroup{}, err
	}

	return resp.SecurityGroup, nil
}
