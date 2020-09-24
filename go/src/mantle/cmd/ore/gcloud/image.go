// Copyright 2015 CoreOS, Inc.
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

package gcloud

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/net/context"
)

var (
	cmdImage = &cobra.Command{
		Use:   "list-images --prefix=<prefix>",
		Short: "List images in GCE",
		Run:   runImage,
	}

	imagePrefix string
)

func init() {
	cmdImage.Flags().StringVar(&imagePrefix, "prefix", "", "prefix to filter list by")
	GCloud.AddCommand(cmdImage)
}

func runImage(cmd *cobra.Command, args []string) {
	if len(args) != 0 {
		fmt.Fprintf(os.Stderr, "Unrecognized args in plume list cmd: %v\n", args)
		os.Exit(2)
	}

	images, err := api.ListImages(context.Background(), imagePrefix, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	for _, image := range images {
		fmt.Printf("%v\n", image.Name)
	}
}
