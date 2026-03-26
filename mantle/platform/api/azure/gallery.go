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
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute"
)

// truncateFCOSVersion converts an FCOS version like 42.20250526.2.1
// into an Azure-compatible gallery image version string like 42.20250526.1.
// The mapping is:
//
//	<major>.<yyyymmdd>.<stream>.<rev> -> <major>.<yyyymmdd>.<rev>
//
// See: https://github.com/coreos/fedora-coreos-tracker/issues/148#issuecomment-2963690654
func truncateFCOSVersion(version string) (string, error) {
	parts := strings.Split(version, ".")
	if len(parts) != 4 {
		return "", fmt.Errorf("unexpected FCOS version format %q", version)
	}
	return parts[0] + "." + parts[1] + "." + parts[3], nil
}

type GalleryProfileConfig struct {
	Version string
	Tags    map[string]*string
}

func (a *API) resolveGalleryProfile(galleryProfile, version string) (GalleryProfileConfig, error) {
	cfg := GalleryProfileConfig{
		Version: version,
	}

	switch galleryProfile {
	case "":
		return cfg, nil
	case "fedora-community":
		truncated, err := truncateFCOSVersion(version)
		if err != nil {
			return GalleryProfileConfig{}, err
		}
		cfg.Version = truncated
		// Tag the image with the full build ID (pre-truncated). Images uploaded to the
		// Fedora community gallery need to be tagged with, at least, `owner: CoreOS`.
		// The `delete: never` tag is added so we can manage when these images are deleted.
		// https://github.com/coreos/fedora-coreos-tracker/issues/148#issuecomment-3612864751
		cfg.Tags = map[string]*string{
			"real_version": to.Ptr(version),
			"owner":        to.Ptr("CoreOS"),
			"delete":       to.Ptr("never"),
		}
		return cfg, nil
	default:
		return GalleryProfileConfig{}, fmt.Errorf("unsupported gallery profile %q", galleryProfile)
	}
}

// isNotFound returns true if the error is an Azure API error with a 404 status code
// This is helpful when trying to determine if Azure resources already exist since a 404
// error on Get() means the resource does not exist.
func isNotFound(err error) bool {
	var respErr *azcore.ResponseError
	if errors.As(err, &respErr) {
		return respErr.StatusCode == http.StatusNotFound
	}
	return false
}

func (a *API) CreateGalleryImage(name, galleryName, resourceGroup, sourceImageID, architecture, version, galleryProfile string) (armcompute.GalleryImageVersion, error) {
	ctx := context.Background()

	_, err := a.galClient.Get(ctx, resourceGroup, galleryName, nil)
	if err == nil {
		plog.Infof("gallery %q already exists in resource group %q; skipping creation", galleryName, resourceGroup)
	} else if isNotFound(err) {
		// Create the Image Gallery if it doesn't already exist.
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
	} else {
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

	profileCfg, err := a.resolveGalleryProfile(galleryProfile, version)
	if err != nil {
		return armcompute.GalleryImageVersion{}, err
	}

	_, err = a.galImgClient.Get(ctx, resourceGroup, galleryName, name, nil)
	if err == nil {
		plog.Infof("gallery image definition %q already exists in gallery %q; skipping creation", name, galleryName)
	} else if isNotFound(err) {
		// Create a Gallery Image Definition if it doesn't already exist.
		galleryImagePoller, err := a.galImgClient.BeginCreateOrUpdate(ctx, resourceGroup, galleryName, name, armcompute.GalleryImage{
			Location: &a.opts.Location,
			Properties: &armcompute.GalleryImageProperties{
				OSState:          to.Ptr(armcompute.OperatingSystemStateTypesGeneralized),
				OSType:           to.Ptr(armcompute.OperatingSystemTypesLinux),
				HyperVGeneration: to.Ptr(armcompute.HyperVGeneration(armcompute.HyperVGenerationV2)),
				Identifier: &armcompute.GalleryImageIdentifier{
					Publisher: &a.opts.Publisher,
					Offer:     &a.opts.Offer,
					SKU:       to.Ptr(name),
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
	} else {
		return armcompute.GalleryImageVersion{}, err
	}

	_, err = a.galImgVerClient.Get(ctx, resourceGroup, galleryName, name, profileCfg.Version, nil)
	if err == nil {
		// The gallery image version already exists, we can't create it again.
		return armcompute.GalleryImageVersion{}, fmt.Errorf("gallery image version %q already exists for image %q", profileCfg.Version, name)
	}
	if !isNotFound(err) {
		return armcompute.GalleryImageVersion{}, err
	}

	// Create a Gallery Image Version
	imageVersionPoller, err := a.galImgVerClient.BeginCreateOrUpdate(ctx, resourceGroup, galleryName, name, profileCfg.Version, armcompute.GalleryImageVersion{
		Location: &a.opts.Location,
		Properties: &armcompute.GalleryImageVersionProperties{
			StorageProfile: &armcompute.GalleryImageVersionStorageProfile{
				Source: &armcompute.GalleryArtifactVersionSource{
					ID: to.Ptr(sourceImageID),
				},
			},
		},
		Tags: profileCfg.Tags,
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

func (a *API) DeleteGalleryImageVersion(imageName, version, resourceGroup, galleryName, galleryProfile string, deleteDefinition bool) error {
	ctx := context.Background()

	profileCfg, err := a.resolveGalleryProfile(galleryProfile, version)
	if err != nil {
		return err
	}

	_, err = a.galImgVerClient.Get(ctx, resourceGroup, galleryName, imageName, profileCfg.Version, nil)
	if err == nil {
		poller, err := a.galImgVerClient.BeginDelete(ctx, resourceGroup, galleryName, imageName, profileCfg.Version, nil)
		if err != nil {
			return err
		}
		_, err = poller.PollUntilDone(ctx, nil)
		if err != nil {
			return err
		}

		plog.Printf("Gallery image version %q for image %q in gallery %q in resource group %q removed", profileCfg.Version, imageName, galleryName, resourceGroup)

	} else if isNotFound(err) {
		plog.Printf("Gallery image version %q for image %q not found in gallery %q in resource group %q; nothing to delete", profileCfg.Version, imageName, galleryName, resourceGroup)
	} else {
		return err
	}

	// Delete the gallery image definition if requested.
	// Gallery images definitions can only be deleted if they have no nested versions.
	if deleteDefinition {
		_, err = a.galImgClient.Get(ctx, resourceGroup, galleryName, imageName, nil)
		if err == nil {
			poller, err := a.galImgClient.BeginDelete(ctx, resourceGroup, galleryName, imageName, nil)
			if err != nil {
				return err
			}
			_, err = poller.PollUntilDone(ctx, nil)
			if err != nil {
				return err
			}

			plog.Printf("Gallery image definition %q in gallery %q in resource group %q removed", imageName, galleryName, resourceGroup)

		} else if isNotFound(err) {
			plog.Printf("Gallery image definition %q not found in gallery %q in resource group %q; nothing to delete", imageName, galleryName, resourceGroup)
		} else {
			return err
		}
	}

	return nil
}
