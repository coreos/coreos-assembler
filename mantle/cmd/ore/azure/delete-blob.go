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

import "github.com/spf13/cobra"

var (
	cmdDeleteBlob = &cobra.Command{
		Use:   "delete-blob",
		Short: "Upload a blob to Azure storage",
		Run:   runDeleteBlob,
	}

	// delete blob options
	dbo struct {
		storageacct string
		container   string
		blob        string
	}
)

func init() {
	sv := cmdDeleteBlob.Flags().StringVar

	sv(&dbo.storageacct, "storage-account", "kola", "storage account name")
	sv(&dbo.container, "container", "vhds", "container name")
	sv(&dbo.blob, "blob-name", "", "name of the blob")
	sv(&resourceGroup, "resource-group", "kola", "resource group name that owns the storage account")

	Azure.AddCommand(cmdDeleteBlob)
}

func runDeleteBlob(cmd *cobra.Command, args []string) {

	if err := api.SetupClients(); err != nil {
		plog.Fatalf("setting up clients: %v\n", err)
	}

	kr, err := api.GetStorageServiceKeys(dbo.storageacct, resourceGroup)
	if err != nil {
		plog.Fatalf("Fetching storage service keys failed: %v", err)
	}

	if kr.Keys == nil || len(*kr.Keys) == 0 {
		plog.Fatalf("No storage service keys found")
	}

	k := (*kr.Keys)[0]
	exists, err := api.BlobExists(dbo.storageacct, *k.Value, dbo.container, dbo.blob)
	if err != nil {
		plog.Fatalf("Checking if blob exists failed: %v", err)
	}

	if !exists {
		plog.Infof("Blob doesn't exist. No need to delete.")
	} else {
		plog.Infof("Deleting blob.")
		err = api.DeleteBlob(dbo.storageacct, *k.Value, dbo.container, dbo.blob)
		if err != nil {
			plog.Fatalf("Deleting blob failed: %v", err)
		}
	}
}
