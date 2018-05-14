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
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/management"
	"github.com/Azure/azure-sdk-for-go/storage"
	"github.com/coreos/pkg/capnslog"

	"github.com/coreos/mantle/auth"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "platform/api/azure")
)

type API struct {
	client management.Client
	opts   *Options
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

	profiles, err := auth.ReadAzureProfile(opts.AzureProfile)
	if err != nil {
		return nil, fmt.Errorf("couldn't read Azure profile: %v", err)
	}

	subOpts := profiles.SubscriptionOptions(opts.AzureSubscription)
	if subOpts == nil {
		return nil, fmt.Errorf("Azure subscription named %q doesn't exist in %q", opts.AzureSubscription, opts.AzureProfile)
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
