// Copyright 2025 Red Hat
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
	"fmt"
	"runtime"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute"

	"github.com/coreos/coreos-assembler/mantle/util"
)

func (a *API) CreateGalleryImage(name, galleryName, resourceGroup, sourceImageID, architecture string) (armcompute.GalleryImageVersion, error) {
	ctx := context.Background()

	// Ensure the Azure Shared Image Gallery exists. BeginCreateOrUpdate will create the gallery
	// in the specified resource group if it doesn't already exist, or update it if it does.
	// Since no properties are being changed here, this acts as a no-op if the gallery does exist.
	// Note: the gallery's location is immutable. If a gallery with the same name exists in a different
	// location within the same resource group, the operation will fail.
	galleryPoller, err := a.galClient.BeginCreateOrUpdate(ctx, resourceGroup, galleryName, armcompute.Gallery{
		Location: &a.opts.Location,
	}, nil)
	if err != nil {
		return armcompute.GalleryImageVersion{}, err
	}
	_, err = galleryPoller.PollUntilDone(context.Background(), nil)
	if err != nil {
		return armcompute.GalleryImageVersion{}, err
	}

	// enable NVMe support for Gen2 images only. NVMe support is not available on Gen1 images.
	// DiskControllerTypes is set to SCSI by default for Gen1 images.
	galleryImageFeatures := []*armcompute.GalleryImageFeature{
		{
			Name:  to.Ptr("DiskControllerTypes"),
			Value: to.Ptr("SCSI,NVMe"),
		},
	}

	var azureArch armcompute.Architecture
	if architecture == "" {
		architecture = runtime.GOARCH
	}
	switch architecture {
	case "amd64", "x86_64":
		azureArch = armcompute.ArchitectureX64
	case "arm64", "aarch64":
		azureArch = armcompute.ArchitectureArm64
	default:
		return armcompute.GalleryImageVersion{}, fmt.Errorf("unsupported azure architecture %q", architecture)
	}

	// Create a Gallery Image Definition with the specified Hyper-V generation (V1 or V2).
	galleryImagePoller, err := a.galImgClient.BeginCreateOrUpdate(ctx, resourceGroup, galleryName, name, armcompute.GalleryImage{
		Location: &a.opts.Location,
		Properties: &armcompute.GalleryImageProperties{
			OSState:          to.Ptr(armcompute.OperatingSystemStateTypesGeneralized),
			OSType:           to.Ptr(armcompute.OperatingSystemTypesLinux),
			HyperVGeneration: to.Ptr(armcompute.HyperVGeneration(armcompute.HyperVGenerationV2)),
			Identifier: &armcompute.GalleryImageIdentifier{
				Publisher: &a.opts.Publisher,
				Offer:     to.Ptr(name),
				SKU:       to.Ptr(util.RandomName("sku")),
			},
			Features:     galleryImageFeatures,
			Architecture: &azureArch,
		},
	}, nil)
	if err != nil {
		return armcompute.GalleryImageVersion{}, err
	}
	_, err = galleryImagePoller.PollUntilDone(context.Background(), nil)
	if err != nil {
		return armcompute.GalleryImageVersion{}, err
	}

	// Create a Gallery Image Version
	versionName := "1.0.0"
	imageVersionPoller, err := a.galImgVerClient.BeginCreateOrUpdate(ctx, resourceGroup, galleryName, name, versionName, armcompute.GalleryImageVersion{
		Location: &a.opts.Location,
		Properties: &armcompute.GalleryImageVersionProperties{
			StorageProfile: &armcompute.GalleryImageVersionStorageProfile{
				Source: &armcompute.GalleryArtifactVersionSource{
					ID: to.Ptr(sourceImageID),
				},
			},
		},
	}, nil)
	if err != nil {
		return armcompute.GalleryImageVersion{}, err
	}
	imageVersionResponse, err := imageVersionPoller.PollUntilDone(context.Background(), nil)
	if err != nil {
		return armcompute.GalleryImageVersion{}, err
	}

	return imageVersionResponse.GalleryImageVersion, nil
}

func (a *API) DeleteGalleryImage(imageName, resourceGroup, galleryName string) error {
	ctx := context.Background()

	timeout := 5 * time.Minute
	delay := 5 * time.Second
	// There is sometimes a delay in the azure backend where deleted gallery images versions
	// still show within the image definition causing a failure during image deletion. We'll
	// retry the delete command again until a specified timeout to ensure the image is deleted.
	err := util.RetryUntilTimeout(timeout, delay, func() error {
		// Find all image versions in the image definition and delete them.
		// Gallery images can only be deleted if they have no nested resources.
		versionPager := a.galImgVerClient.NewListByGalleryImagePager(resourceGroup, galleryName, imageName, nil)
		for versionPager.More() {
			versionPage, err := versionPager.NextPage(ctx)
			if err != nil {
				return fmt.Errorf("failed to list image versions for %s: %v", imageName, err)
			}
			for _, version := range versionPage.Value {
				poller, err := a.galImgVerClient.BeginDelete(ctx, resourceGroup, galleryName, imageName, *version.Name, nil)
				if err != nil {
					return err
				}
				_, err = poller.PollUntilDone(ctx, nil)
				if err != nil {
					return err
				}
			}
		}

		// delete the gallery image
		poller, err := a.galImgClient.BeginDelete(ctx, resourceGroup, galleryName, imageName, nil)
		if err != nil {
			return err
		}
		_, err = poller.PollUntilDone(ctx, nil)
		if err != nil {
			return err
		}

		return nil
	})

	return err

}

func (a *API) DeleteGallery(galleryName, resourceGroup string) error {
	ctx := context.Background()

	timeout := 10 * time.Minute
	delay := 5 * time.Second
	// There is sometimes a delay in the azure backend where deleted gallery images still show
	// within the gallery causing a failure during gallery deletion. We'll retry the delete
	// command again until a specified timeout to ensure the gallery is deleted.
	err := util.RetryUntilTimeout(timeout, delay, func() error {
		// Find all images in the gallery and delete them.
		// Galleries can only be deleted if they have no nested resources.
		imagePager := a.galImgClient.NewListByGalleryPager(resourceGroup, galleryName, nil)
		for imagePager.More() {
			page, err := imagePager.NextPage(ctx)
			if err != nil {
				return fmt.Errorf("failed to get image definitions")
			}
			for _, image := range page.Value {
				err := a.DeleteGalleryImage(*image.Name, resourceGroup, galleryName)
				if err != nil {
					return fmt.Errorf("Couldn't delete gallery image: %v\n", err)
				}
			}
		}

		// delete the gallery
		poller, err := a.galClient.BeginDelete(ctx, resourceGroup, galleryName, nil)
		if err != nil {
			return err
		}
		_, err = poller.PollUntilDone(ctx, nil)
		if err != nil {
			return err
		}

		return nil
	})

	return err

}
