// Copyright 2025 Red Hat
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

	"github.com/spf13/cobra"
)

var (
	cmdDeleteGalleryImage = &cobra.Command{
		Use:     "delete-gallery-image",
		Short:   "Delete Azure Gallery image",
		Long:    "Remove a Shared Image Gallery image from Azure.",
		RunE:    runDeleteGalleryImage,
		Aliases: []string{"delete-gallery-image-arm"},

		SilenceUsage: true,
	}

	deleteDefinition bool
)

func init() {
	sv := cmdDeleteGalleryImage.Flags().StringVar
	bv := cmdDeleteGalleryImage.Flags().BoolVar

	sv(&imageName, "gallery-image-name", "", "gallery image name")
	sv(&resourceGroup, "resource-group", "kola", "resource group name")
	sv(&galleryName, "gallery-name", "kola", "gallery name")
	sv(&version, "version", "", "The azure gallery image version")
	sv(&galleryProfile, "coreos-gallery-profile", "", "The CoreOS specific gallery profile to apply to the image on upload")
	bv(&deleteDefinition, "delete-definition", false, "delete the gallery image definition after deleting the specified version")

	Azure.AddCommand(cmdDeleteGalleryImage)
}

func runDeleteGalleryImage(cmd *cobra.Command, args []string) error {
	if imageName == "" {
		return fmt.Errorf("must supply --gallery-image-name")
	}

	if version == "" {
		return fmt.Errorf("must supply --version")
	}

	if err := api.SetupClients(); err != nil {
		return fmt.Errorf("setting up clients: %v\n", err)
	}

	err := api.DeleteGalleryImageVersion(imageName, version, resourceGroup, galleryName, galleryProfile, deleteDefinition)
	if err != nil {
		return fmt.Errorf("Couldn't delete gallery image version: %v\n", err)
	}

	return nil
}
