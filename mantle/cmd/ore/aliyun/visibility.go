// Copyright 2021 Red Hat
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
	"strings"

	"github.com/spf13/cobra"
)

var (
	cmdVisibility = &cobra.Command{
		Use:   "visibility <region:image...>",
		Short: "Change the visibility of images on aliyun",
		Long: `Change the visibilityu of images on aliyun.

Images can be marked as publicly available or private.
`,
		RunE: changeVisibility,

		SilenceUsage: true,
	}

	private bool
	public  bool
)

func init() {
	Aliyun.AddCommand(cmdVisibility)
	cmdVisibility.Flags().BoolVar(&private, "private", false, "mark image as private")
	cmdVisibility.Flags().BoolVar(&public, "public", false, "mark image as publicly available")

}

func changeVisibility(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("Specify one ore more region:image pairs.\n")
	}

	if (public && private) || (!public && !private) {
		return fmt.Errorf("Must only specify --public or --private.\n")
	}

	supportedRegions, err := API.ListRegions()
	if err != nil {
		return fmt.Errorf("could not list regions: %v\n", err)
	}
	supportedMap := make(map[string]bool)
	for _, r := range supportedRegions {
		supportedMap[r] = true
	}

	for _, pair := range args {
		if !strings.Contains(pair, ":") {
			return fmt.Errorf("Argument isn't a valid region:image pair: %v\n", pair)
		}

		v := strings.Split(pair, ":")
		if len(v) > 2 {
			return fmt.Errorf("Argument isn't a valid region:image pair: %v\n", pair)
		}

		region, image := v[0], v[1]

		if !supportedMap[region] {
			return fmt.Errorf("%v is not a valid region\n", region)
		}

		// default bool is false
		var visibility bool
		if public {
			visibility = true
		}
		err = API.ChangeVisibility(region, image, visibility)
		if err != nil {
			return fmt.Errorf("Couldn't change the visibility of image %v in region %v: %v", image, region, err)
		}
	}

	return nil
}
