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

package packet

import (
	"fmt"

	"github.com/coreos/mantle/auth"
	"github.com/coreos/mantle/cli"
	"github.com/coreos/mantle/platform/api/packet"
	"github.com/coreos/pkg/capnslog"
	"github.com/spf13/cobra"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "ore/packet")

	Packet = &cobra.Command{
		Use:   "packet [command]",
		Short: "Packet machine utilities",
	}

	API     *packet.API
	options packet.Options
)

func init() {
	Packet.PersistentFlags().StringVar(&options.ConfigPath, "config-file", "", "config file (default \"~/"+auth.PacketConfigPath+"\")")
	Packet.PersistentFlags().StringVar(&options.Profile, "profile", "", "profile (default \"default\")")
	Packet.PersistentFlags().StringVar(&options.ApiKey, "api-key", "", "API key (overrides config file)")
	Packet.PersistentFlags().StringVar(&options.Project, "project", "", "project UUID (overrides config file)")
	cli.WrapPreRun(Packet, preflightCheck)
}

func preflightCheck(cmd *cobra.Command, args []string) error {
	plog.Debugf("Running Packet preflight check")
	api, err := packet.New(&options)
	if err != nil {
		return fmt.Errorf("could not create Packet client: %v", err)
	}
	if err := api.PreflightCheck(); err != nil {
		return fmt.Errorf("could not complete Packet preflight check: %v", err)
	}

	plog.Debugf("Preflight check success; we have liftoff")
	API = api
	return nil
}
