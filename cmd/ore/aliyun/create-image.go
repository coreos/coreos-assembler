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

	"github.com/spf13/cobra"

	"github.com/coreos/mantle/util"
)

var (
	cmdCreate = &cobra.Command{
		Use:   "create-image",
		Short: "Create image on aliyun",
		Long: `Upload an image to aliyun.

After a successful run, the final line of output will be the ID of the image.
`,
		RunE: runCreate,
	}

	bucket       string
	diskSize     string
	path         string
	name         string
	format       string
	device       string
	description  string
	architecture string
	force        bool
	sizeInspect  bool
)

func init() {
	Aliyun.AddCommand(cmdCreate)
	cmdCreate.Flags().StringVar(&path, "file", "", "path to image")
	cmdCreate.Flags().StringVar(&diskSize, "disk-size-gib", "8", "image disk size in GiB")
	cmdCreate.Flags().BoolVar(&sizeInspect, "disk-size-inspect", false, "set image disk size to size of local file")
	cmdCreate.Flags().StringVar(&bucket, "bucket", "", "object storage bucket")
	cmdCreate.Flags().StringVar(&format, "format", "qcow2", "image format")
	cmdCreate.Flags().StringVar(&device, "device", "/dev/xvda", "image device")
	cmdCreate.Flags().StringVar(&description, "description", "", "image description")
	cmdCreate.Flags().StringVar(&architecture, "architecture", "x86_64", "image architecture")
	cmdCreate.Flags().StringVar(&name, "name", "", "image name")
	cmdCreate.Flags().BoolVar(&force, "force", false, "overwrite any existing object storage")
}

func runCreate(cmd *cobra.Command, args []string) error {
	if sizeInspect {
		imageInfo, err := util.GetImageInfo(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Unable to query size of disk: %v\n", err)
			os.Exit(1)
		}
		plog.Debugf("Image size: %v\n", imageInfo.VirtualSize)
		const GiB = 1024 * 1024 * 1024
		diskSizeGiB := uint(imageInfo.VirtualSize / GiB)
		// Round up if there's leftover
		if imageInfo.VirtualSize%GiB > 0 {
			diskSizeGiB += 1
		}

		diskSize = fmt.Sprintf("%d", diskSizeGiB)
	}

	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not open image file %v: %v\n", path, err)
		os.Exit(1)
	}
	defer f.Close()

	err = API.PutObject(f, bucket, name, force)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Uploading image to object storage: %v\n", err)
		os.Exit(1)
	}

	id, err := API.ImportImage(format, bucket, name, diskSize, device, name, description, architecture)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Couldn't create image: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(id)
	return nil
}
