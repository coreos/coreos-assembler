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
	"os"

	"github.com/spf13/cobra"
)

var (
	cmdDelete = &cobra.Command{
		Use:   "delete-kola-vcn",
		Short: "Delete OCI networking",
		Long:  "Remove kola virtual cloud network from an OCI account",
		Run:   runDeleteVCN,
	}
)

func init() {
	OCI.AddCommand(cmdDelete)
}

func runDeleteVCN(cmd *cobra.Command, args []string) {
	if len(args) != 0 {
		fmt.Fprintf(os.Stderr, "Unrecognized args in ore clear cmd: %v\n", args)
		os.Exit(2)
	}

	vcn, err := API.GetVCN("kola")
	if err != nil {
		fmt.Fprintf(os.Stderr, "A Virtual Cloud Network named `kola` doesn't exist!\n")
		os.Exit(1)
	}

	subnets, err := API.ListSubnets(vcn.ID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Getting Subnets: %v\n", err)
		os.Exit(1)
	}

	for _, subnet := range subnets {
		err = API.DeleteSubnet(subnet.ID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Deleting Subnet %s: %v\n", subnet.DisplayName, err)
			os.Exit(1)
		}
	}

	rts, err := API.ListRouteTables(vcn.ID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Getting Route Tables: %v\n", err)
		os.Exit(1)
	}

	for _, rt := range rts {
		if rt.DisplayName != fmt.Sprintf("Default Route Table for %s", vcn.DisplayName) {
			err = API.DeleteRouteTable(rt.ID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Deleting Route Table %s: %v\n", rt.DisplayName, err)
				os.Exit(1)
			}
		}
	}

	igws, err := API.ListInternetGateways(vcn.ID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Getting Internet Gateways: %v\n", err)
		os.Exit(1)
	}

	for _, igw := range igws {
		err = API.DeleteInternetGateway(igw.ID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Deleting Internet Gateway %s: %v\n", igw.DisplayName, err)
			os.Exit(1)
		}
	}

	secLists, err := API.ListSecurityLists(vcn.ID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Getting Security Lists: %v\n", err)
		os.Exit(1)
	}

	for _, secList := range secLists {
		if secList.DisplayName != fmt.Sprintf("Default Security List for %s", vcn.DisplayName) {
			err = API.DeleteSecurityList(secList.ID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Deleting Security List %s: %v\n", secList.DisplayName, err)
				os.Exit(1)
			}
		}
	}

	err = API.DeleteVCN(vcn.ID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Deleting Virtual Cloud Network %s: %v\n", vcn.DisplayName, err)
		os.Exit(1)
	}
}
