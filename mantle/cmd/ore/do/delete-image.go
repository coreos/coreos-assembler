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

package do

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	cmdDeleteImage = &cobra.Command{
		Use:   "delete-image [options]",
		Short: "Delete image",
		Long:  `Delete an image.`,
		RunE:  runDeleteImage,

		SilenceUsage: true,
	}
)

func init() {
	DO.AddCommand(cmdDeleteImage)
	cmdDeleteImage.Flags().StringVarP(&imageName, "name", "n", "", "image name")
}

func runDeleteImage(cmd *cobra.Command, args []string) error {
	if len(args) != 0 {
		fmt.Fprintf(os.Stderr, "Unrecognized args in do delete-image cmd: %v\n", args)
		os.Exit(2)
	}

	if err := deleteImage(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	return nil
}

func deleteImage() error {
	if imageName == "" {
		return fmt.Errorf("Image name must be specified")
	}

	ctx := context.Background()

	image, err := API.GetUserImage(ctx, imageName, false)
	if err != nil {
		return err
	}

	if err := API.DeleteImage(ctx, image.ID); err != nil {
		return err
	}

	return nil
}
