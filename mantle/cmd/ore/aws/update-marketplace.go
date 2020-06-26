// Copyright 2020 Red Hat, Inc.
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

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

var (
	cmdUpdateMarketplaceCatalog = &cobra.Command{
		Use:   "update-marketplace",
		Short: "Update AWS Marketplace",
		Long:  `Update AWS Marketplace Catalog Entity with newly uploaded AMI`,
		RunE:  runMarketplaceCatalogUpdate,

		Example: `ore aws update-marketplace --entity-type="RHCOSIMG@1" \
	  --entity-id="9EXAMPLE-0123-4567-8901-74eEXAMPLE47" \
	  --newAmi="0123456789012" \
	  --newVersion="45.81.202004022007-0"`,

		SilenceUsage: true,
	}

	marketplaceEntityType string
	marketplaceEntityId	  string
	marketplaceNewAmi     string
	marketplaceNewVersion string
)

func init() {
	AWS.AddCommand(cmdMarketplaceCatalog)
	cmdMarketplaceCatalog.Flags().StringVar(&marketplaceEntityType, "entity-type", "", "type of the entity to update")
	cmdMarketplaceCatalog.Flags().StringVar(&marketplaceEntityId, "entity-id", "", "id of the entity to update")
	cmdMarketplaceCatalog.Flags().StringVar(&marketplaceNewAmi, "new-ami", "", "new ami to add to entity")
	cmdMarketplaceCatalog.Flags().StringVar(&marketplaceNewVersion, "new-version", "", "new version to add to entity")
}

func runMarketplaceCatalogUpdate(cmd *cobra.Command, args []string) error {
	err := API.AddAmiToMarketplace(marketplaceEntityType, marketplaceEntityId, marketplaceNewAmi, marketplaceNewVersion)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Couldn't update marketplace catalog: %v\n", err)
		os.Exit(1)
	}
	return nil
}

