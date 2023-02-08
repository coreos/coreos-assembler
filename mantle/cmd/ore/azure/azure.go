// Copyright 2023 Red Hat
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
	"github.com/coreos/pkg/capnslog"
	"github.com/spf13/cobra"

	"github.com/coreos/coreos-assembler/mantle/auth"
	"github.com/coreos/coreos-assembler/mantle/cli"
	"github.com/coreos/coreos-assembler/mantle/platform/api/azure"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/coreos-assembler/mantle", "ore/azure")

	Azure = &cobra.Command{
		Use:   "azure [command]",
		Short: "azure image and vm utilities",
	}

	azureCredentials string
	azureLocation    string

	api *azure.API
)

func init() {
	cli.WrapPreRun(Azure, preauth)

	sv := Azure.PersistentFlags().StringVar
	sv(&azureCredentials, "azure-credentials", "", "Azure credentials file location (default \"~/"+auth.AzureCredentialsPath+"\")")
	sv(&azureLocation, "azure-location", "westus", "Azure location (default \"westus\")")
}

func preauth(cmd *cobra.Command, args []string) error {
	plog.Printf("Creating Azure API...")

	a, err := azure.New(&azure.Options{
		AzureCredentials: azureCredentials,
		Location:         azureLocation,
	})
	if err != nil {
		plog.Fatalf("Failed to create Azure API: %v", err)
	}

	api = a
	return nil
}
