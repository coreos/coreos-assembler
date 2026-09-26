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
	"strings"

	"github.com/spf13/cobra"
)

var (
	cmdTagBlob = &cobra.Command{
		Use:   "tag-blob",
		Short: "Tag a Azure storage blob",
		Long:  "tag a storage blob on Azure.",
		Run:   runSetMetadata,
	}

	bmo struct {
		tags          []string
		storageacct   string
		container     string
		blob          string
		resourceGroup string
	}
)

func init() {
	sv := cmdTagBlob.Flags().StringVar
	ssv := cmdTagBlob.Flags().StringSliceVar
	sv(&bmo.storageacct, "storage-account", "kola", "storage account name")
	sv(&bmo.container, "container", "vhds", "container name")
	sv(&bmo.blob, "blob-name", "", "name of the blob")
	sv(&bmo.resourceGroup, "resource-group", "kola", "resource group name")
	ssv(&bmo.tags, "tags", []string{}, "list of key=value tags to attach to the Azure storage blob")
	Azure.AddCommand(cmdTagBlob)
}

func runSetMetadata(cmd *cobra.Command, args []string) {
	if bmo.blob == "" {
		fmt.Fprintf(os.Stderr, "Provide --blob-name to tag\n")
		os.Exit(1)
	}

	if len(bmo.tags) < 1 {
		fmt.Fprintf(os.Stderr, "Provide --tag to tag\n")
		os.Exit(1)
	}

	if err := api.SetupClients(); err != nil {
		fmt.Fprintf(os.Stderr, "setting up clients: %v\n", err)
		os.Exit(1)
	}

	kr, err := api.GetStorageServiceKeys(bmo.storageacct, bmo.resourceGroup)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Fetching storage service keys failed: %v\n", err)
		os.Exit(1)
	}

	if len(kr.Keys) == 0 {
		fmt.Fprintf(os.Stderr, "No storage service keys found")
		os.Exit(1)
	}
	k := kr.Keys
	key := k[0].Value

	tagMap := make(map[string]*string)
	for _, tag := range bmo.tags {
		splitTag := strings.SplitN(tag, "=", 2)
		if len(splitTag) != 2 {
			fmt.Fprintf(os.Stderr, "invalid tag format; should be key=value, not %v\n", tag)
			os.Exit(1)
		}
		key, value := splitTag[0], splitTag[1]
		tagMap[key] = &value
	}

	err = api.SetBlobMetadata(bmo.storageacct, *key, bmo.container, bmo.blob, tagMap)
	if err != nil {
		fmt.Fprintf(os.Stderr, "couldn't create image tags: %v", err)
		os.Exit(1)
	}
}
