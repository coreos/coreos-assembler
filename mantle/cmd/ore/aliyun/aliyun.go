// Copyright 2019 Red Hat
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

package aliyun

import (
	"fmt"
	"os"

	"github.com/coreos/pkg/capnslog"
	"github.com/spf13/cobra"

	"github.com/coreos/coreos-assembler/mantle/cli"
	"github.com/coreos/coreos-assembler/mantle/platform/api/aliyun"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/coreos-assembler/mantle", "ore/aliyun")

	Aliyun = &cobra.Command{
		Use:   "aliyun [command]",
		Short: "aliyun machine utilities",
	}

	API     *aliyun.API
	options aliyun.Options
)

func init() {
	defaultConfigPath := os.Getenv("ALIYUN_CONFIG_FILE")

	Aliyun.PersistentFlags().StringVar(&options.ConfigPath, "config-file", defaultConfigPath, "config file (default \""+defaultConfigPath+"\")")
	Aliyun.PersistentFlags().StringVar(&options.Profile, "profile", "", "profile (default \"default\")")
	Aliyun.PersistentFlags().StringVar(&options.Region, "region", "", "region")
	cli.WrapPreRun(Aliyun, preflightCheck)
}

func preflightCheck(cmd *cobra.Command, args []string) error {
	plog.Debugf("Running aliyun preflight check")
	api, err := aliyun.New(&options)
	if err != nil {
		return fmt.Errorf("could not create aliyun client: %v", err)
	}

	plog.Debugf("Preflight check success; we have liftoff")
	API = api
	return nil
}
