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

package oci

import (
	"fmt"

	"github.com/oracle/bmcs-go-sdk"
)

func (a *API) GetVCN(name string) (*baremetal.VirtualNetwork, error) {
	vcns, err := a.client.ListVirtualNetworks(a.opts.CompartmentID, nil)
	if err != nil {
		return nil, err
	}

	for _, v := range vcns.VirtualNetworks {
		if name == v.DisplayName {
			return &v, nil
		}
	}

	return nil, fmt.Errorf("couldn't find Virtual Network %s", name)
}

func (a *API) ListAvailabilityDomains() ([]baremetal.AvailabilityDomain, error) {
	ads, err := a.client.ListAvailabilityDomains(a.opts.CompartmentID)
	if err != nil {
		return nil, err
	}

	return ads.AvailabilityDomains, err
}

func (a *API) CreateVCN(name, cidrBlock string) (*baremetal.VirtualNetwork, error) {
	return a.client.CreateVirtualNetwork(cidrBlock, a.opts.CompartmentID, &baremetal.CreateVcnOptions{
		CreateOptions: baremetal.CreateOptions{
			DisplayNameOptions: baremetal.DisplayNameOptions{
				DisplayName: name,
			},
		},
		DnsLabel: name,
	})
}

func (a *API) DeleteVCN(ID string) error {
	return a.client.DeleteVirtualNetwork(ID, nil)
}

func (a *API) CreateSubnet(subdomain, availabilityDomain, cidrBlock, vcnID, securityListID, routeTableID string) (*baremetal.Subnet, error) {
	return a.client.CreateSubnet(availabilityDomain, cidrBlock, a.opts.CompartmentID, vcnID, &baremetal.CreateSubnetOptions{
		SecurityListIDs: []string{securityListID},
		RouteTableID:    routeTableID,
		DNSLabel:        subdomain,
	})
}

func (a *API) CreateInternetGateway(vcnID string) (*baremetal.InternetGateway, error) {
	return a.client.CreateInternetGateway(a.opts.CompartmentID, vcnID, true, nil)
}

func (a *API) ListSecurityLists(vcnID string) ([]baremetal.SecurityList, error) {
	secLists, err := a.client.ListSecurityLists(a.opts.CompartmentID, vcnID, nil)
	if err != nil {
		return nil, err
	}
	return secLists.SecurityLists, nil
}

func (a *API) DeleteSecurityList(ID string) error {
	return a.client.DeleteSecurityList(ID, nil)
}

func (a *API) ListInternetGateways(vcnID string) ([]baremetal.InternetGateway, error) {
	igws, err := a.client.ListInternetGateways(a.opts.CompartmentID, vcnID, nil)
	if err != nil {
		return nil, err
	}
	return igws.Gateways, nil
}

func (a *API) DeleteInternetGateway(ID string) error {
	return a.client.DeleteInternetGateway(ID, nil)
}

func (a *API) ListRouteTables(vcnID string) ([]baremetal.RouteTable, error) {
	rts, err := a.client.ListRouteTables(a.opts.CompartmentID, vcnID, nil)
	if err != nil {
		return nil, err
	}
	return rts.RouteTables, nil
}

func (a *API) DeleteRouteTable(ID string) error {
	return a.client.DeleteRouteTable(ID, nil)
}

func (a *API) CreateDefaultSecurityList(vcnID string) (*baremetal.SecurityList, error) {
	ingressRules := []baremetal.IngressSecurityRule{
		{
			// Allow all TCP on private network
			Protocol: "6",
			Source:   "10.0.0.0/16",
			TCPOptions: &baremetal.TCPOptions{
				DestinationPortRange: baremetal.PortRange{
					Min: 1,
					Max: 65535,
				},
			},
		},
		{
			// Allow all UDP on private network
			Protocol: "17",
			Source:   "10.0.0.0/16",
			UDPOptions: &baremetal.UDPOptions{
				DestinationPortRange: baremetal.PortRange{
					Min: 1,
					Max: 65535,
				},
			},
		},
		{
			// Allow all ICMP on private network
			Protocol: "1",
			Source:   "10.0.0.0/16",
		},
		{
			// Default setting:
			// open inbound TCP traffic to port 22
			// from any source to allow for SSH
			Protocol: "6",
			Source:   "0.0.0.0/0",
			TCPOptions: &baremetal.TCPOptions{
				DestinationPortRange: baremetal.PortRange{
					Min: 22,
					Max: 22,
				},
			},
		},
		{
			// Default setting:
			// allow type 3 ICMP to the machine
			// "Destination Unreachable" from any source
			// to allow for MTU negotiation
			Protocol: "1",
			Source:   "0.0.0.0/0",
			ICMPOptions: &baremetal.ICMPOptions{
				Code: 4,
				Type: 3,
			},
		},
	}

	egressRules := []baremetal.EgressSecurityRule{
		{
			Destination: "0.0.0.0/0",
			Protocol:    "all",
		},
	}

	return a.client.CreateSecurityList(a.opts.CompartmentID, vcnID, egressRules, ingressRules, nil)
}

func (a *API) ListSubnets(vcnID string) ([]baremetal.Subnet, error) {
	subnets, err := a.client.ListSubnets(a.opts.CompartmentID, vcnID, nil)
	if err != nil {
		return nil, err
	}
	return subnets.Subnets, nil

}

func (a *API) DeleteSubnet(ID string) error {
	return a.client.DeleteSubnet(ID, nil)
}

func (a *API) getSubnetOnVCN(vcnID string) (*baremetal.Subnet, error) {
	subnets, err := a.ListSubnets(vcnID)
	if err != nil {
		return nil, err
	}

	if len(subnets) < 1 {
		return nil, fmt.Errorf("could't find Subnet")
	}
	return &subnets[0], nil
}

func (a *API) CreateDefaultRouteTable(vcnID, igwID string) (*baremetal.RouteTable, error) {
	return a.client.CreateRouteTable(a.opts.CompartmentID, vcnID, []baremetal.RouteRule{
		{
			CidrBlock:       "0.0.0.0/0",
			NetworkEntityID: igwID,
		},
	}, nil)
}
