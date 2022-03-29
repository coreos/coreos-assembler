// Copyright 2022 Red Hat
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
	"os"

	"github.com/spf13/cobra"
)

var (
	cmdDeleteImage = &cobra.Command{
		Use:     "delete-image",
		Short:   "Delete Azure image",
		Long:    "Remove an image from Azure.",
		RunE:    runDeleteImage,
		Aliases: []string{"delete-image-arm"},

		SilenceUsage: true,
	}
)

func init() {
	sv := cmdDeleteImage.Flags().StringVar

	sv(&imageName, "image-name", "", "image name")
	sv(&resourceGroup, "resource-group", "kola", "resource group name")

	Azure.AddCommand(cmdDeleteImage)
}

func runDeleteImage(cmd *cobra.Command, args []string) error {
	if err := api.SetupClients(); err != nil {
		fmt.Fprintf(os.Stderr, "setting up clients: %v\n", err)
		os.Exit(1)
	}

	err := api.DeleteImage(imageName, resourceGroup)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Couldn't delete image: %v\n", err)
		os.Exit(1)
	}

	plog.Printf("Image %q in resource group %q removed", imageName, resourceGroup)
	return nil
}
