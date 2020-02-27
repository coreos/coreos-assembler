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
	"os"

	"github.com/coreos/mantle/sdk"
	"github.com/spf13/cobra"
)

var (
	cmdCreate = &cobra.Command{
		Use:   "create-image",
		Short: "Create image on OpenStack",
		Long: `Upload an image to OpenStack.

After a successful run, the final line of output will be the ID of the image.
`,
		RunE: runCreate,
	}

	path string
	name string
)

func init() {
	OpenStack.AddCommand(cmdCreate)
	cmdCreate.Flags().StringVar(&path, "file",
		sdk.BuildRoot()+"/images/amd64-usr/latest/coreos_production_openstack_image.img",
		"path to CoreOS image (build with: ./image_to_vm.sh --format=openstack ...)")
	cmdCreate.Flags().StringVar(&name, "name", "", "image name")
}

func runCreate(cmd *cobra.Command, args []string) error {
	id, err := API.UploadImage(name, path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Couldn't create image: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(id)
	return nil
}
