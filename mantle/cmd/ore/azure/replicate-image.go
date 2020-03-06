// Copyright 2016 CoreOS, Inc.
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

	"github.com/coreos/go-semver/semver"
	"github.com/spf13/cobra"
)

var (
	cmdReplicateImage = &cobra.Command{
		Use:   "replicate-image image",
		Short: "Replicate an OS image in Azure",
		RunE:  runReplicateImage,

		SilenceUsage: true,
	}

	defaultRegions = []string{
		"East US",
		"West US",
		"South Central US",
		"Central US",
		"North Central US",
		"East US 2",
		"North Europe",
		"West Europe",
		"Southeast Asia",
		"East Asia",
		"Japan West",
		"Japan East",
		"Brazil South",
		"Australia Southeast",
		"Australia East",
		"Central India",
		"South India",
		"West India",
		"Canada Central",
		"Canada East",
		"UK North",
		"UK South 2",
		"West US 2",
		"West Central US",
		"UK West",
		"UK South",
		"Central US EUAP",
		"East US 2 EUAP",
	}

	// replicate image options
	rio struct {
		offer   string
		sku     string
		version string
		regions []string
	}
)

func init() {
	sv := cmdReplicateImage.Flags().StringVar

	sv(&rio.offer, "offer", "CoreOS", "Azure image product name")
	sv(&rio.sku, "sku", "", "Azure image SKU (stable, beta, alpha for CoreOS)")
	sv(&rio.version, "version", "", "Azure image version")

	cmdReplicateImage.Flags().StringSliceVar(&rio.regions, "region", defaultRegions,
		"Azure regions to replicate to")

	Azure.AddCommand(cmdReplicateImage)
}

func runReplicateImage(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("expecting 1 argument, got %d", len(args))
	}

	if rio.offer == "" {
		return fmt.Errorf("offer name is required")
	}

	if rio.sku == "" {
		return fmt.Errorf("sku is required")
	}

	if rio.version == "" {
		return fmt.Errorf("version is required")
	}

	_, err := semver.NewVersion(rio.version)
	if err != nil {
		return fmt.Errorf("version is not valid semver: %v", err)
	}

	return api.ReplicateImage(args[0], rio.offer, rio.sku, rio.version, rio.regions...)
}
