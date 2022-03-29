// Copyright 2022 Red Hat
// Copyright 2018 CoreOS, Inc.
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
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	cmdCreateImage = &cobra.Command{
		Use:     "create-image",
		Short:   "Create Azure image",
		Long:    "Create Azure image from a blob url",
		RunE:    runCreateImage,
		Aliases: []string{"create-image-arm"},

		SilenceUsage: true,
	}

	imageName     string
	blobUrl       string
	resourceGroup string
)

func init() {
	sv := cmdCreateImage.Flags().StringVar

	sv(&imageName, "image-name", "", "image name")
	sv(&blobUrl, "image-blob", "", "source blob url")
	sv(&resourceGroup, "resource-group", "kola", "resource group name")

	Azure.AddCommand(cmdCreateImage)
}

func runCreateImage(cmd *cobra.Command, args []string) error {
	if err := api.SetupClients(); err != nil {
		fmt.Fprintf(os.Stderr, "setting up clients: %v\n", err)
		os.Exit(1)
	}
	img, err := api.CreateImage(imageName, resourceGroup, blobUrl)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Couldn't create image: %v\n", err)
		os.Exit(1)
	}
	if img.ID == nil {
		fmt.Fprintf(os.Stderr, "received nil image\n")
		os.Exit(1)
	}
	err = json.NewEncoder(os.Stdout).Encode(&struct {
		ID       *string
		Location *string
	}{
		ID:       img.ID,
		Location: img.Location,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Couldn't encode result: %v\n", err)
		os.Exit(1)
	}
	return nil
}
