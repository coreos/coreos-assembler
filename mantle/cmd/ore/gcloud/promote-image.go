// Copyright 2020 Red Hat
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

package gcloud

import (
	"github.com/coreos/mantle/platform/api/gcloud"
	"github.com/spf13/cobra"
	"golang.org/x/net/context"
)

var (
	cmdPromoteImage = &cobra.Command{
		Use:   "promote-image",
		Short: "Promote GCP image in image family",
		Long:  "Promote GCP image in image family and deprecate all others",
		Run:   runPromoteImage,
	}

	promoteImageName   string
	promoteImageFamily string
)

func init() {
	cmdPromoteImage.Flags().StringVar(&promoteImageName, "image", "", "GCP image name")
	cmdPromoteImage.Flags().StringVar(&promoteImageFamily, "family", "", "GCP image family to promote within")
	GCloud.AddCommand(cmdPromoteImage)
}

func deprecateImage(name string, state gcloud.DeprecationState, replacement string) {
	plog.Infof("Changing deprecation state of image: %v -> %v", name, state)
	pending, err := api.DeprecateImage(name, state, replacement)
	if err == nil {
		err = pending.Wait()
	}
	// New if statement to check err on api.DeprecateImage or pending.Wait()
	if err != nil {
		plog.Fatalf("Changing deprecation state of image failed: %v\n", err)
	}
}

func runPromoteImage(cmd *cobra.Command, args []string) {
	// Check that the user provided an image
	if promoteImageName == "" {
		plog.Fatal("Must provide an image name via --image")
	}
	// Check that the user provided an image family
	if promoteImageFamily == "" {
		plog.Fatal("Must provide an image family via --family")
	}

	plog.Infof("Attempting to promote %v in family %v",
		promoteImageName, promoteImageFamily)

	// Get all images in the image family
	images, err := api.ListImages(context.Background(), "", promoteImageFamily)
	if err != nil {
		plog.Fatal(err)
	}

	// Make sure the specified image exists in the specified image family
	found := false
	for _, image := range images {
		if image.Name == promoteImageName {
			found = true
		}
	}
	if !found {
		plog.Fatalf("The image (%v) must be in the image family (%v)",
			promoteImageName, promoteImageFamily)
	}

	// First undeprecate the image we want to promote
	deprecateImage(promoteImageName, gcloud.DeprecationStateActive, "")

	// Next deprecate all other images in the image family
	// that need to be deprecated.
	for _, image := range images {
		// don't deprecate the image we just undeprecated
		if image.Name == promoteImageName {
			continue
		}
		// Some debug messages which are useful when needed.
		// This triggers the deprecation lint in golangci-lint because the
		// docstring for the `Deprecated` field starts with "Deprecated: ". The
		// docstring was tweaked to not trigger this, so we can drop this in the
		// next vendor bump. See:
		// https://github.com/googleapis/google-api-go-client/issues/767.
		// nolint
		if image.Deprecated != nil {
			plog.Debugf("Deprecation state for %v is %v",
				image.Name, image.Deprecated.State)
		} else {
			plog.Debugf("Deprecation state is nil for %v", image.Name)
		}
		// Perform the deprecation if the image is not already deprecated.
		// We detect if it is active by checking if it either doesn't
		// have any deprecation state or if it is explicitly ACTIVE.
		// nolint (see comment above)
		if image.Deprecated == nil ||
			image.Deprecated.State == string(gcloud.DeprecationStateActive) {
			deprecateImage(
				image.Name,
				gcloud.DeprecationStateDeprecated,
				promoteImageName,
			)
		}
	}
}
