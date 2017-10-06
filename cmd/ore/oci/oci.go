// Copyright 2017 CoreOS, Inc.
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

package oci

import (
	"github.com/coreos/mantle/cli"
	"github.com/coreos/mantle/platform/api/oci"
	"github.com/coreos/pkg/capnslog"
	"github.com/spf13/cobra"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "ore/oci")

	OCI = &cobra.Command{
		Use:   "oci [command]",
		Short: "oci image and vm utilities",
	}

	API     *oci.API
	options oci.Options
)

func init() {
	cli.WrapPreRun(OCI, preauth)
}

func preauth(cmd *cobra.Command, args []string) error {
	api, err := oci.New(&options)
	if err != nil {
		return err
	}

	API = api

	return nil
}
