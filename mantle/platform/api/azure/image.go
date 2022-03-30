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

// DeleteImage removes Azure image
func (a *API) DeleteImage(name, resourceGroup string) error {
	_, err := a.imgClient.Delete(resourceGroup, name, nil)
	return err
}
