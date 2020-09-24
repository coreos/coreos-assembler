// Copyright 2016 CoreOS, Inc.
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

package azure

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	cmdShareImage = &cobra.Command{
		Use:   "share-image image-name",
		Short: "Set permissions on an azure OS image",
		RunE:  runShareImage,

		SilenceUsage: true,
	}

	sharePermission string
)

func init() {
	sv := cmdShareImage.Flags().StringVar

	sv(&sharePermission, "permission", "public", "Image permission (one of: public, msdn, private)")

	Azure.AddCommand(cmdShareImage)
}

func runShareImage(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("expecting 1 argument, got %d", len(args))
	}

	if sharePermission == "" {
		return fmt.Errorf("permission is required")
	}

	return api.ShareImage(args[0], sharePermission)
}
