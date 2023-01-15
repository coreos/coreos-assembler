// Copyright 2018 Red Hat
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

package openstack

import (
	"fmt"

	"github.com/coreos/pkg/capnslog"
	"github.com/spf13/cobra"

	"github.com/coreos/coreos-assembler/mantle/cli"
	"github.com/coreos/coreos-assembler/mantle/platform/api/openstack"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/coreos-assembler/mantle", "ore/openstack")

	OpenStack = &cobra.Command{
		Use:   "openstack [command]",
		Short: "OpenStack machine utilities",
	}

	API     *openstack.API
	options openstack.Options
)

func init() {
	OpenStack.PersistentFlags().StringVar(&options.ConfigPath, "config-file", "", "Path to a clouds.yaml formatted OpenStack config file. The underlying library defaults to ./clouds.yaml")
	OpenStack.PersistentFlags().StringVar(&options.Profile, "profile", "", "OpenStack profile within clouds.yaml (default \"openstack\")")
	OpenStack.PersistentFlags().StringVar(&options.Region, "region", "", "Override the OpenStack region from the config.")
	cli.WrapPreRun(OpenStack, preflightCheck)
}

func preflightCheck(cmd *cobra.Command, args []string) error {
	plog.Debugf("Running OpenStack preflight check")
	api, err := openstack.New(&options)
	if err != nil {
		return fmt.Errorf("could not create OpenStack client: %v", err)
	}
	if err := api.PreflightCheck(); err != nil {
		return fmt.Errorf("could not complete OpenStack preflight check: %v", err)
	}

	plog.Debugf("Preflight check success; we have liftoff")
	API = api
	return nil
}
