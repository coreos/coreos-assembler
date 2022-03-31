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
	"strings"

	"github.com/Microsoft/azure-vhd-utils/vhdcore/validator"
	"github.com/spf13/cobra"
)

var (
	cmdUploadBlob = &cobra.Command{
		Use:     "upload-blob",
		Short:   "Upload a blob to Azure storage",
		Run:     runUploadBlob,
		Aliases: []string{"upload-blob-arm"},
	}

	// upload blob options
	ubo struct {
		storageacct string
		container   string
		blob        string
		vhd         string
		overwrite   bool
		validate    bool
	}
)

func init() {
	bv := cmdUploadBlob.Flags().BoolVar
	sv := cmdUploadBlob.Flags().StringVar

	bv(&ubo.overwrite, "overwrite", false, "overwrite blob")
	bv(&ubo.validate, "validate", true, "validate blob as VHD file")

	sv(&ubo.storageacct, "storage-account", "kola", "storage account name")
	sv(&ubo.container, "container", "vhds", "container name")
	sv(&ubo.blob, "blob-name", "", "name of the blob")
	sv(&ubo.vhd, "file", "", "path to CoreOS VHD image")
	sv(&resourceGroup, "resource-group", "kola", "resource group name that owns the storage account")

	Azure.AddCommand(cmdUploadBlob)
}

func runUploadBlob(cmd *cobra.Command, args []string) {
	if ubo.vhd == "" {
		plog.Fatal("--file is required")
	}
	if ubo.blob == "" {
		plog.Fatal("--blob-name is required")
	}

	if err := api.SetupClients(); err != nil {
		plog.Fatalf("setting up clients: %v\n", err)
	}

	if ubo.validate {
		plog.Printf("Validating VHD %q", ubo.vhd)
		if !strings.HasSuffix(strings.ToLower(ubo.blob), ".vhd") {
			plog.Fatalf("Blob name should end with .vhd")
		}

		if !strings.HasSuffix(strings.ToLower(ubo.vhd), ".vhd") {
			plog.Fatalf("Image should end with .vhd")
		}

		if err := validator.ValidateVhd(ubo.vhd); err != nil {
			plog.Fatal(err)
		}

		if err := validator.ValidateVhdSize(ubo.vhd); err != nil {
			plog.Fatal(err)
		}
	}

	kr, err := api.GetStorageServiceKeys(ubo.storageacct, resourceGroup)
	if err != nil {
		plog.Fatalf("Fetching storage service keys failed: %v", err)
	}

	if kr.Keys == nil || len(*kr.Keys) == 0 {
		plog.Fatalf("No storage service keys found")
	}

	//only use the first service key to avoid uploading twice
	//see https://github.com/coreos/coreos-assembler/pull/1849
	k := (*kr.Keys)[0]
	if err := api.UploadBlob(ubo.storageacct, *k.Value, ubo.vhd, ubo.container, ubo.blob, ubo.overwrite); err != nil {
		plog.Fatalf("Uploading blob failed: %v", err)
	}

	err = json.NewEncoder(os.Stdout).Encode(&struct {
		URL string
	}{
		URL: fmt.Sprintf("https://%s.blob.core.windows.net/%s/%s", ubo.storageacct, ubo.container, ubo.blob),
	})

	if err != nil {
		plog.Fatal(err)
	}
}
