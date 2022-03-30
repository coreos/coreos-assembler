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
	"bufio"
	"fmt"
	"net/http"
	"strings"

	"github.com/Azure/azure-sdk-for-go/arm/compute"
)

func (a *API) CreateImage(name, resourceGroup, blobURI string) (compute.Image, error) {
	_, err := a.imgClient.CreateOrUpdate(resourceGroup, name, compute.Image{
		Name:     &name,
		Location: &a.opts.Location,
		ImageProperties: &compute.ImageProperties{
			StorageProfile: &compute.ImageStorageProfile{
				OsDisk: &compute.ImageOSDisk{
					OsType:  compute.Linux,
					OsState: compute.Generalized,
					BlobURI: &blobURI,
				},
			},
		},
	}, nil)
	if err != nil {
		return compute.Image{}, err
	}

	return a.imgClient.Get(resourceGroup, name, "")
}

// resolveImage is used to ensure that either a Version or DiskURI
// are provided present for a run. If neither is given via arguments
// it attempts to parse the Version from the version.txt in the Sku's
// release bucket.
func (a *API) resolveImage() error {
	// immediately return if the version has been set or if the channel
	// is not set via the Sku (this happens in ore)
	if a.opts.DiskURI != "" || a.opts.Version != "" || a.opts.Sku == "" {
		return nil
	}

	resp, err := http.DefaultClient.Get(fmt.Sprintf("https://%s.release.core-os.net/amd64-usr/current/version.txt", a.opts.Sku))
	if err != nil {
		return fmt.Errorf("unable to fetch release bucket %v version: %v", a.opts.Sku, err)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.SplitN(scanner.Text(), "=", 2)
		if len(line) != 2 {
			continue
		}
		if line[0] == "COREOS_VERSION" {
			a.opts.Version = line[1]
			return nil
		}
	}

	return fmt.Errorf("couldn't find COREOS_VERSION in version.txt")
}

// DeleteImage removes Azure image
func (a *API) DeleteImage(name, resourceGroup string) error {
	_, err := a.imgClient.Delete(resourceGroup, name, nil)
	return err
}
