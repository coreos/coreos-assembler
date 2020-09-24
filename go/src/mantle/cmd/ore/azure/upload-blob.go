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
	"fmt"
	"strings"

	"github.com/Microsoft/azure-vhd-utils/vhdcore/validator"
	"github.com/spf13/cobra"
)

var (
	cmdUploadBlob = &cobra.Command{
		Use:   "upload-blob storage-account container blob-name file",
		Short: "Upload a blob to Azure storage",
		Run:   runUploadBlob,
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

	bv(&ubo.overwrite, "overwrite", false, "overwrite blob")
	bv(&ubo.validate, "validate", true, "validate blob as VHD file")

	Azure.AddCommand(cmdUploadBlob)
}

func runUploadBlob(cmd *cobra.Command, args []string) {
	if len(args) != 4 {
		plog.Fatalf("Expecting 4 arguments, got %d", len(args))
	}

	ubo.storageacct = args[0]
	ubo.container = args[1]
	ubo.blob = args[2]
	ubo.vhd = args[3]

	if ubo.validate {
		plog.Printf("Validating VHD %q", ubo.vhd)
		if !strings.HasSuffix(strings.ToLower(ubo.blob), ".vhd") {
			plog.Fatalf("Blob name should end with .vhd")
		}

		if err := validator.ValidateVhd(ubo.vhd); err != nil {
			plog.Fatal(err)
		}

		if err := validator.ValidateVhdSize(ubo.vhd); err != nil {
			plog.Fatal(err)
		}
	}

	kr, err := api.GetStorageServiceKeys(ubo.storageacct)
	if err != nil {
		plog.Fatalf("Fetching storage service keys failed: %v", err)
	}

	if err := api.UploadBlob(ubo.storageacct, kr.PrimaryKey, ubo.vhd, ubo.container, ubo.blob, ubo.overwrite); err != nil {
		plog.Fatalf("Uploading blob failed: %v", err)
	}

	uri := fmt.Sprintf("https://%s.blob.core.windows.net/%s/%s", ubo.storageacct, ubo.container, ubo.blob)

	plog.Printf("Blob uploaded to %q", uri)
}
