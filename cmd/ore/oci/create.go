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
	cmdCreate = &cobra.Command{
		Use:   "create-kola-vcn",
		Short: "Create OCI networking",
		Long:  "Create kola virtual cloud network in an OCI account",
		Run:   runCreateVCN,
	}
)

func init() {
	OCI.AddCommand(cmdCreate)
}

func runCreateVCN(cmd *cobra.Command, args []string) {
	if len(args) != 0 {
		fmt.Fprintf(os.Stderr, "Unrecognized args in ore setup cmd: %v\n", args)
		os.Exit(2)
	}

	_, err := API.GetVCN("kola")
	if err == nil {
		fmt.Fprintf(os.Stderr, "A Virtual Cloud Network named `kola` already exists!\n")
		os.Exit(1)
	}

	vcn, err := API.CreateVCN("kola", "10.0.0.0/16")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Creating VCN: %v\n", err)
		os.Exit(1)
	}

	secList, err := API.CreateDefaultSecurityList(vcn.ID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Creating default Security List: %v\n", err)
		os.Exit(1)
	}

	igw, err := API.CreateInternetGateway(vcn.ID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Creating Internet Gateway: %v\n", err)
		os.Exit(1)
	}

	rt, err := API.CreateDefaultRouteTable(vcn.ID, igw.ID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Creating default Route Table: %v\n", err)
		os.Exit(1)
	}

	ads, err := API.ListAvailabilityDomains()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Listing Availability Domains: %v\n", err)
		os.Exit(1)
	}

	ad := ads[0]

	_, err = API.CreateSubnet("kola1", ad.Name, "10.0.0.0/16", vcn.ID, secList.ID, rt.ID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Creating Subnet: %v\n", err)
		os.Exit(1)
	}
}
