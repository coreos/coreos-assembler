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
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/arm/compute"
	"github.com/Azure/azure-sdk-for-go/arm/network"
	"github.com/Azure/azure-sdk-for-go/arm/resources/resources"
	armStorage "github.com/Azure/azure-sdk-for-go/arm/storage"
	"github.com/Azure/azure-sdk-for-go/management"
	"github.com/Azure/azure-sdk-for-go/storage"
	"github.com/Azure/go-autorest/autorest/azure/auth"
	"github.com/coreos/pkg/capnslog"

	internalAuth "github.com/coreos/mantle/auth"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "platform/api/azure")
)

type API struct {
	client     management.Client
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

// New creates a new Azure client. If no publish settings file is provided or
// can't be parsed, an anonymous client is created.
func New(opts *Options) (*API, error) {
	conf := management.DefaultConfig()
	conf.APIVersion = "2015-04-01"

	if opts.ManagementURL != "" {
		conf.ManagementURL = opts.ManagementURL
	}

	if opts.StorageEndpointSuffix == "" {
		opts.StorageEndpointSuffix = storage.DefaultBaseURL
	}

	profiles, err := internalAuth.ReadAzureProfile(opts.AzureProfile)
	if err != nil {
		return nil, fmt.Errorf("couldn't read Azure profile: %v", err)
	}

	subOpts := profiles.SubscriptionOptions(opts.AzureSubscription)
	if subOpts == nil {
		return nil, fmt.Errorf("Azure subscription named %q doesn't exist in %q", opts.AzureSubscription, opts.AzureProfile)
	}

	if os.Getenv("AZURE_AUTH_LOCATION") == "" {
		if opts.AzureAuthLocation == "" {
			user, err := user.Current()
			if err != nil {
				return nil, err
			}
			opts.AzureAuthLocation = filepath.Join(user.HomeDir, internalAuth.AzureAuthPath)
		}
		// TODO: Move to Flight once built to allow proper unsetting
		os.Setenv("AZURE_AUTH_LOCATION", opts.AzureAuthLocation)
	}

	if opts.SubscriptionID == "" {
		opts.SubscriptionID = subOpts.SubscriptionID
	}

	if opts.SubscriptionName == "" {
		opts.SubscriptionName = subOpts.SubscriptionName
	}

	if opts.ManagementURL == "" {
		opts.ManagementURL = subOpts.ManagementURL
	}

	if opts.ManagementCertificate == nil {
		opts.ManagementCertificate = subOpts.ManagementCertificate
	}

	if opts.StorageEndpointSuffix == "" {
		opts.StorageEndpointSuffix = subOpts.StorageEndpointSuffix
	}

	client, err := management.NewClientFromConfig(opts.SubscriptionID, opts.ManagementCertificate, conf)
	if err != nil {
		return nil, fmt.Errorf("failed to create azure client: %v", err)
	}

	api := &API{
		client: client,
		opts:   opts,
	}

	err = api.resolveImage()
	if err != nil {
		return nil, fmt.Errorf("failed to resolve image: %v", err)
	}

	return api, nil
}

func (a *API) SetupClients() error {
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
