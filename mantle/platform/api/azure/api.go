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
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/arm/compute"
	"github.com/Azure/azure-sdk-for-go/arm/network"
	"github.com/Azure/azure-sdk-for-go/arm/resources/resources"
	armStorage "github.com/Azure/azure-sdk-for-go/arm/storage"
	"github.com/Azure/azure-sdk-for-go/storage"
	"github.com/Azure/go-autorest/autorest/azure/auth"
	"github.com/coreos/pkg/capnslog"

	internalAuth "github.com/coreos/mantle/auth"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "platform/api/azure")
)

type API struct {
	rgClient   resources.GroupsClient
	imgClient  compute.ImagesClient
	compClient compute.VirtualMachinesClient
	netClient  network.VirtualNetworksClient
	subClient  network.SubnetsClient
	ipClient   network.PublicIPAddressesClient
	intClient  network.InterfacesClient
	accClient  armStorage.AccountsClient
	opts       *Options
}

var azureAuthLocationFile = &auth.File{
	ClientID:                "",
	ClientSecret:            "",
	SubscriptionID:          "",
	TenantID:                "",
	ActiveDirectoryEndpoint: "https://login.microsoftonline.com",
	ResourceManagerEndpoint: "https://management.azure.com/",
	GraphResourceID:         "https://graph.windows.net/",
	SQLManagementEndpoint:   "https://management.core.windows.net:8443/",
	GalleryEndpoint:         "https://gallery.azure.com/",
	ManagementEndpoint:      "https://management.core.windows.net/",
}

// New creates a new Azure client. If no publish settings file is provided or
// can't be parsed, an anonymous client is created.
func New(opts *Options) (*API, error) {
	if opts.StorageEndpointSuffix == "" {
		opts.StorageEndpointSuffix = storage.DefaultBaseURL
	}

	azCreds, err := internalAuth.ReadAzureCredentials(opts.AzureCredentials)
	if err != nil {
		return nil, fmt.Errorf("couldn't read Azure Credentials file: %v", err)
	}

	// Populate the mssing info in the azureAuthLocationFile struct,
	// this will be used in the SetupClients() function below.
	azureAuthLocationFile.ClientID = azCreds.ClientID
	azureAuthLocationFile.ClientSecret = azCreds.ClientSecret
	azureAuthLocationFile.SubscriptionID = azCreds.SubscriptionID
	azureAuthLocationFile.TenantID = azCreds.TenantID

	opts.SubscriptionID = azCreds.SubscriptionID
	api := &API{
		opts: opts,
	}

	if opts.Sku != "" && opts.DiskURI == "" && opts.Version == "" {
		return nil, fmt.Errorf("SKU set to %q but Disk URI and version not set; can't resolve", opts.Sku)
	}

	return api, nil
}

func (a *API) SetupClients() error {
	// First write out a AZURE_AUTH_LOCATION tmp file with the necessary
	// data since GetClientSetup requires it to be read in from a file
	// pointed to by the $AZURE_AUTH_LOCATION env var.
	tmpf, err := os.CreateTemp("", "azureAuth.json")
	if err != nil {
		return fmt.Errorf("couldn't create temporary file: %v", err)
	}
	defer os.Remove(tmpf.Name()) // clean up tmp file
	buf, err := json.Marshal(azureAuthLocationFile)
	if err != nil {
		return fmt.Errorf("couldn't marshal AzureAuth data to JSON: %v", err)
	}
	if _, err = tmpf.Write(buf); err != nil {
		return fmt.Errorf("couldn't write JSON to temporary file: %v", err)
	}
	if err = tmpf.Close(); err != nil {
		return fmt.Errorf("couldn't close temporary file: %v", err)
	}
	os.Setenv("AZURE_AUTH_LOCATION", tmpf.Name())

	auther, err := auth.GetClientSetup(resources.DefaultBaseURI)
	if err != nil {
		return err
	}
	a.rgClient = resources.NewGroupsClientWithBaseURI(auther.BaseURI, auther.SubscriptionID)
	a.rgClient.Authorizer = auther

	auther, err = auth.GetClientSetup(compute.DefaultBaseURI)
	if err != nil {
		return err
	}
	a.imgClient = compute.NewImagesClientWithBaseURI(auther.BaseURI, auther.SubscriptionID)
	a.imgClient.Authorizer = auther
	a.compClient = compute.NewVirtualMachinesClientWithBaseURI(auther.BaseURI, auther.SubscriptionID)
	a.compClient.Authorizer = auther

	auther, err = auth.GetClientSetup(network.DefaultBaseURI)
	if err != nil {
		return err
	}
	a.netClient = network.NewVirtualNetworksClientWithBaseURI(auther.BaseURI, auther.SubscriptionID)
	a.netClient.Authorizer = auther
	a.subClient = network.NewSubnetsClientWithBaseURI(auther.BaseURI, auther.SubscriptionID)
	a.subClient.Authorizer = auther
	a.ipClient = network.NewPublicIPAddressesClientWithBaseURI(auther.BaseURI, auther.SubscriptionID)
	a.ipClient.Authorizer = auther
	a.intClient = network.NewInterfacesClientWithBaseURI(auther.BaseURI, auther.SubscriptionID)
	a.intClient.Authorizer = auther

	auther, err = auth.GetClientSetup(armStorage.DefaultBaseURI)
	if err != nil {
		return err
	}
	a.accClient = armStorage.NewAccountsClientWithBaseURI(auther.BaseURI, auther.SubscriptionID)
	a.accClient.Authorizer = auther

	return nil
}

func randomName(prefix string) string {
	b := make([]byte, 5)
	rand.Read(b)
	return fmt.Sprintf("%s-%x", prefix, b)
}

func (a *API) GC(gracePeriod time.Duration) error {
	durationAgo := time.Now().Add(-1 * gracePeriod)

	listGroups, err := a.ListResourceGroups("")
	if err != nil {
		return fmt.Errorf("listing resource groups: %v", err)
	}

	for _, l := range *listGroups.Value {
		if strings.HasPrefix(*l.Name, "kola-cluster") {
			createdAt := *(*l.Tags)["createdAt"]
			timeCreated, err := time.Parse(time.RFC3339, createdAt)
			if err != nil {
				return fmt.Errorf("error parsing time: %v", err)
			}
			if !timeCreated.After(durationAgo) {
				if err = a.TerminateResourceGroup(*l.Name); err != nil {
					return err
				}
			}
		}
	}

	return nil
}
