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
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute"
)

func (a *API) CreateImage(name, resourceGroup, blobURI string) (armcompute.Image, error) {
	ctx := context.Background()
	poller, err := a.imgClient.BeginCreateOrUpdate(ctx, resourceGroup, name, armcompute.Image{
		Name:     &name,
		Location: &a.opts.Location,
		Properties: &armcompute.ImageProperties{
			HyperVGeneration: to.Ptr(armcompute.HyperVGenerationTypesV1),
			StorageProfile: &armcompute.ImageStorageProfile{
				OSDisk: &armcompute.ImageOSDisk{
					OSType:  to.Ptr(armcompute.OperatingSystemTypesLinux),
					OSState: to.Ptr(armcompute.OperatingSystemStateTypesGeneralized),
					BlobURI: &blobURI,
				},
			},
		},
	}, nil)
	if err != nil {
		return armcompute.Image{}, err
	}
	resp, err := poller.PollUntilDone(context.Background(), nil)
	if err != nil {
		return armcompute.Image{}, err
	}
	return resp.Image, nil
}

// DeleteImage removes Azure image
func (a *API) DeleteImage(name, resourceGroup string) error {
	ctx := context.Background()
	poller, err := a.imgClient.BeginDelete(ctx, resourceGroup, name, nil)
	if err != nil {
		return err
	}
	_, err = poller.PollUntilDone(ctx, nil)
	return err
}
