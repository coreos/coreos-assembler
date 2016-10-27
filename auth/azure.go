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

package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"

	"github.com/coreos/mantle/platform/api/azure"
)

const AzureProfilePath = ".azure/azureProfile.json"

type AzureEnvironment struct {
	ActiveDirectoryEndpointURL                        string `json:"activeDirectoryEndpointUrl"`
	ActiveDirectoryGraphAPIVersion                    string `json:"activeDirectoryGraphApiVersion"`
	ActiveDirectoryGraphResourceID                    string `json:"activeDirectoryGraphResourceId"`
	ActiveDirectoryResourceID                         string `json:"activeDirectoryResourceId"`
	AzureDataLakeAnalyticsCatalogAndJobEndpointSuffix string `json:"azureDataLakeAnalyticsCatalogAndJobEndpointSuffix"`
	AzureDataLakeStoreFileSystemEndpointSuffix        string `json:"azureDataLakeStoreFileSystemEndpointSuffix"`
	GalleryEndpointURL                                string `json:"galleryEndpointUrl"`
	KeyVaultDNSSuffix                                 string `json:"keyVaultDnsSuffix"`
	ManagementEndpointURL                             string `json:"managementEndpointUrl"`
	Name                                              string `json:"name"`
	PortalURL                                         string `json:"portalUrl"`
	PublishingProfileURL                              string `json:"publishingProfileUrl"`
	ResourceManagerEndpointURL                        string `json:"resourceManagerEndpointUrl"`
	SqlManagementEndpointURL                          string `json:"sqlManagementEndpointUrl"`
	SqlServerHostnameSuffix                           string `json:"sqlServerHostnameSuffix"`
	StorageEndpointSuffix                             string `json:"storageEndpointSuffix"`
}

type AzureManagementCertificate struct {
	Cert string `json:"cert"`
	Key  string `json:"key"`
}

type AzureSubscription struct {
	EnvironmentName       string                     `json:"environmentName"`
	ID                    string                     `json:"id"`
	IsDefault             bool                       `json:"isDefault"`
	ManagementCertificate AzureManagementCertificate `json:"managementCertificate"`
	ManagementEndpointURL string                     `json:"managementEndpointUrl"`
	Name                  string                     `json:"name"`
	RegisteredProviders   []string                   `json:"registeredProviders"`
	State                 string                     `json:"state"`
}

// AzureProfile represents a parsed Azure Profile Configuration File.
type AzureProfile struct {
	Environments  []AzureEnvironment  `json:"environments"`
	Subscriptions []AzureSubscription `json:"subscriptions"`
}

// AsOptions converts all subscriptions into a slice of azure.Options.
// If there is an environment with a name matching the subscription, that environment's storage endpoint will be copied to the options.
func (ap *AzureProfile) AsOptions() []azure.Options {
	var o []azure.Options

	for _, sub := range ap.Subscriptions {
		newo := azure.Options{
			SubscriptionName:      sub.Name,
			SubscriptionID:        sub.ID,
			ManagementURL:         sub.ManagementEndpointURL,
			ManagementCertificate: bytes.Join([][]byte{[]byte(sub.ManagementCertificate.Key), []byte(sub.ManagementCertificate.Cert)}, []byte("\n")),
		}

		// find the storage endpoint for the subscription
		for _, e := range ap.Environments {
			if e.Name == sub.EnvironmentName {
				newo.StorageEndpointSuffix = e.StorageEndpointSuffix
				break
			}
		}

		o = append(o, newo)
	}

	return o
}

// SubscriptionOptions returns the name subscription in the Azure profile as a azure.Options struct.
// If the subscription name is "", the first subscription is returned.
// If there are no subscriptions or the named subscription is not found, SubscriptionOptions returns nil.
func (ap *AzureProfile) SubscriptionOptions(name string) *azure.Options {
	opts := ap.AsOptions()

	if len(opts) == 0 {
		return nil
	}

	if name == "" {
		return &opts[0]
	} else {
		for _, o := range ap.AsOptions() {
			if o.SubscriptionName == name {
				return &o
			}
		}
	}

	return nil
}

// ReadAzureProfile decodes an Azure Profile, as created by the Azure Cross-platform CLI.
//
// If path is empty, $HOME/.azure/azureProfile.json is read.
func ReadAzureProfile(path string) (*AzureProfile, error) {
	if path == "" {
		user, err := user.Current()
		if err != nil {
			return nil, err
		}

		path = filepath.Join(user.HomeDir, AzureProfilePath)
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var ap AzureProfile
	if err := json.NewDecoder(f).Decode(&ap); err != nil {
		return nil, err
	}

	if len(ap.Subscriptions) == 0 {
		return nil, fmt.Errorf("Azure profile %q contains no subscriptions", path)
	}

	return &ap, nil
}
