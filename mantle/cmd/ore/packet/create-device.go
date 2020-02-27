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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/spf13/cobra"

	ctplatform "github.com/coreos/container-linux-config-transpiler/config/platform"
	"github.com/coreos/mantle/platform/conf"
)

var (
	cmdCreateDevice = &cobra.Command{
		Use:   "create-device [options]",
		Short: "Create Packet device",
		Long:  `Create a Packet device.`,
		RunE:  runCreateDevice,
	}
	hostname     string
	userDataPath string
)

func init() {
	Packet.AddCommand(cmdCreateDevice)
	cmdCreateDevice.Flags().StringVar(&options.Facility, "facility", "sjc1", "facility code")
	cmdCreateDevice.Flags().StringVar(&options.Plan, "plan", "", "plan slug (default board-dependent, e.g. \"t1.small.x86\")")
	cmdCreateDevice.Flags().StringVar(&options.Board, "board", "amd64-usr", "Container Linux board")
	cmdCreateDevice.Flags().StringVar(&options.InstallerImageBaseURL, "installer-image-base-url", "", "installer image base URL, non-https (default board-dependent, e.g. \"http://stable.release.core-os.net/amd64-usr/current\")")
	cmdCreateDevice.Flags().StringVar(&options.ImageURL, "image-url", "", "image base URL (default board-dependent, e.g. \"https://alpha.release.core-os.net/amd64-usr/current/coreos_production_packet_image.bin.bz2\")")
	cmdCreateDevice.Flags().StringVar(&hostname, "hostname", "", "hostname to assign to device")
	cmdCreateDevice.Flags().StringVar(&userDataPath, "userdata-file", "", "path to file containing userdata")
}

func runCreateDevice(cmd *cobra.Command, args []string) error {
	if len(args) != 0 {
		fmt.Fprintf(os.Stderr, "Unrecognized args in packet create-device cmd: %v\n", args)
		os.Exit(2)
	}

	userdata := conf.Empty()
	if userDataPath != "" {
		data, err := ioutil.ReadFile(userDataPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Couldn't read userdata file %v: %v\n", userDataPath, err)
			os.Exit(1)
		}
		userdata = conf.Unknown(string(data))
	}
	conf, err := userdata.Render(ctplatform.Packet, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Couldn't parse userdata file %v: %v\n", userDataPath, err)
		os.Exit(1)
	}

	device, err := API.CreateDevice(hostname, conf, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Couldn't create device: %v\n", err)
		os.Exit(1)
	}

	err = json.NewEncoder(os.Stdout).Encode(&struct {
		ID       string `json:"id"`
		Hostname string `json:"hostname"`
		IP       string `json:"public-ip,omitempty"`
	}{
		ID:       device.ID,
		Hostname: device.Hostname,
		IP:       API.GetDeviceAddress(device, 4, true),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Couldn't encode result: %v\n", err)
		os.Exit(1)
	}
	return nil
}
