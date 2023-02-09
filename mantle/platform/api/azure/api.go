// Copyright 2023 Red Hat
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
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/storage/armstorage"

	"github.com/coreos/coreos-assembler/mantle/auth"
)

type API struct {
	azIdCred   *azidentity.DefaultAzureCredential
	rgClient   *armresources.ResourceGroupsClient
	imgClient  *armcompute.ImagesClient
	compClient *armcompute.VirtualMachinesClient
	netClient  *armnetwork.VirtualNetworksClient
	subClient  *armnetwork.SubnetsClient
	ipClient   *armnetwork.PublicIPAddressesClient
	intClient  *armnetwork.InterfacesClient
	accClient  *armstorage.AccountsClient
	opts       *Options
}

// New creates a new Azure client. If no publish settings file is provided or
// can't be parsed, an anonymous client is created.
func New(opts *Options) (*API, error) {
	azCreds, err := auth.ReadAzureCredentials(opts.AzureCredentials)
	if err != nil {
		return nil, fmt.Errorf("couldn't read Azure Credentials file: %v", err)
	}

	opts.SubscriptionID = azCreds.SubscriptionID
	os.Setenv("AZURE_CLIENT_ID", azCreds.ClientID)
	os.Setenv("AZURE_TENANT_ID", azCreds.TenantID)
	os.Setenv("AZURE_CLIENT_SECRET", azCreds.ClientSecret)

	api := &API{
		opts: opts,
	}

	if opts.Sku != "" && opts.DiskURI == "" && opts.Version == "" {
		return nil, fmt.Errorf("SKU set to %q but Disk URI and version not set; can't resolve", opts.Sku)
	}

	return api, nil
}

func (a *API) SetupClients() error {
	var err error
	a.azIdCred, err = azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return err
	}

	a.rgClient, err = armresources.NewResourceGroupsClient(a.opts.SubscriptionID, a.azIdCred, nil)
	if err != nil {
		return err
	}

	a.imgClient, err = armcompute.NewImagesClient(a.opts.SubscriptionID, a.azIdCred, nil)
	if err != nil {
		return err
	}

	a.compClient, err = armcompute.NewVirtualMachinesClient(a.opts.SubscriptionID, a.azIdCred, nil)
	if err != nil {
		return err
	}

	a.netClient, err = armnetwork.NewVirtualNetworksClient(a.opts.SubscriptionID, a.azIdCred, nil)
	if err != nil {
		return err
	}

	a.subClient, err = armnetwork.NewSubnetsClient(a.opts.SubscriptionID, a.azIdCred, nil)
	if err != nil {
		return err
	}

	a.ipClient, err = armnetwork.NewPublicIPAddressesClient(a.opts.SubscriptionID, a.azIdCred, nil)
	if err != nil {
		return err
	}

	a.intClient, err = armnetwork.NewInterfacesClient(a.opts.SubscriptionID, a.azIdCred, nil)
	if err != nil {
		return err
	}

	a.accClient, err = armstorage.NewAccountsClient(a.opts.SubscriptionID, a.azIdCred, nil)
	return err
}

func randomName(prefix string) string {
	b := make([]byte, 5)
	rand.Read(b)
	return fmt.Sprintf("%s-%x", prefix, b)
}

func (a *API) GC(gracePeriod time.Duration) error {
	durationAgo := time.Now().Add(-1 * gracePeriod)

	resourceGroups, err := a.ListResourceGroups()
	if err != nil {
		return fmt.Errorf("listing resource groups: %v", err)
	}

	for _, l := range resourceGroups {
		if strings.HasPrefix(*l.Name, "kola-cluster") {
			terminate := false
			if l.Tags == nil || l.Tags["createdAt"] == nil {
				// If the group name starts with kola-cluster and has
				// no tags OR no createdAt then it failed to properly
				// get created and we should clean it up.
				// https://github.com/coreos/coreos-assembler/issues/3057
				terminate = true
			} else {
				timeCreated, err := time.Parse(time.RFC3339, *l.Tags["createdAt"])
				if err != nil {
					return fmt.Errorf("error parsing time: %v", err)
				}
				if !timeCreated.After(durationAgo) {
					// If the group is older than specified time then gc
					terminate = true
				}
			}
			if terminate {
				if err = a.TerminateResourceGroup(*l.Name); err != nil {
					return err
				}
			}
		}
	}

	return nil
}
