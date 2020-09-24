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
	"io/ioutil"
	"os"
	"os/user"
	"path/filepath"

	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"

	"github.com/coreos/mantle/platform"
)

const (
	AzureAuthPath    = ".azure/credentials.json"
	AzureProfilePath = ".azure/azureProfile.json"
)

// A version of the Options struct from platform/api/azure that only
// contains the ASM values. Otherwise there's a cyclical depdendence
// because platform/api/azure has to import auth to have access to
// the ReadAzureProfile function.
type Options struct {
	*platform.Options

	SubscriptionName string
	SubscriptionID   string

	// Azure API endpoint. If unset, the Azure SDK default will be used.
	ManagementURL         string
	ManagementCertificate []byte

	// Azure Storage API endpoint suffix. If unset, the Azure SDK default will be used.
	StorageEndpointSuffix string
}

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

// AsOptions converts all subscriptions into a slice of Options.
// If there is an environment with a name matching the subscription, that environment's storage endpoint will be copied to the options.
func (ap *AzureProfile) AsOptions() []Options {
	var o []Options

	for _, sub := range ap.Subscriptions {
		newo := Options{
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

// SubscriptionOptions returns the name subscription in the Azure profile as a Options struct.
// If the subscription name is "", the first subscription is returned.
// If there are no subscriptions or the named subscription is not found, SubscriptionOptions returns nil.
func (ap *AzureProfile) SubscriptionOptions(name string) *Options {
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

	contents, err := DecodeBOMFile(path)
	if err != nil {
		return nil, err
	}

	var ap AzureProfile
	if err := json.Unmarshal(contents, &ap); err != nil {
		return nil, err
	}

	if len(ap.Subscriptions) == 0 {
		return nil, fmt.Errorf("Azure profile %q contains no subscriptions", path)
	}

	return &ap, nil
}

func DecodeBOMFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	decoder := unicode.UTF8.NewDecoder()
	reader := transform.NewReader(f, unicode.BOMOverride(decoder))
	return ioutil.ReadAll(reader)
}
