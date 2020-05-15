// Copyright 2020 Red Hat
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

	"github.com/coreos/mantle/platform/api/gcloud"
	"github.com/spf13/cobra"
)

var (
	cmdDeprecateImage = &cobra.Command{
		Use:   "deprecate-image --image=ImageName [--state=DeprecationState] [--replacement=Replacement]",
		Short: "Deprecate GCP image",
		Long:  "Change deprecation status of existing GCP image",
		Run:   runDeprecateImage,
	}

	deprecateImageName        string
	deprecateImageState       string
	deprecateImageReplacement string
)

func init() {
	cmdDeprecateImage.Flags().StringVar(&deprecateImageName, "image", "", "GCP image name")
	cmdDeprecateImage.Flags().StringVar(&deprecateImageState, "state",
		string(gcloud.DeprecationStateDeprecated),
		fmt.Sprintf("Deprecation state must be one of: %s,%s,%s,%s",
			gcloud.DeprecationStateActive,
			gcloud.DeprecationStateDeprecated,
			gcloud.DeprecationStateObsolete,
			gcloud.DeprecationStateDeleted))
	cmdDeprecateImage.Flags().StringVar(&deprecateImageReplacement,
		"replacement", "", "optional: link to replacement for the deprecated image")
	GCloud.AddCommand(cmdDeprecateImage)
}

func runDeprecateImage(cmd *cobra.Command, args []string) {
	// Check that the user provided an image
	if deprecateImageName == "" {
		plog.Fatal("Must provide an image name via --image")
	}

	// Check that the deprecation state is a valid one
	switch gcloud.DeprecationState(deprecateImageState) {
	case gcloud.DeprecationStateActive,
		gcloud.DeprecationStateDeprecated,
		gcloud.DeprecationStateObsolete,
		gcloud.DeprecationStateDeleted:
		// Do nothing, state is valid
	default:
		plog.Fatalf("Specified deprecation state is invalid: %s\n", deprecateImageState)
	}

	plog.Debugf("Attempting to change GCP image deprecation state of %s to %s\n",
		deprecateImageName, deprecateImageState)
	pending, err := api.DeprecateImage(deprecateImageName,
		gcloud.DeprecationState(deprecateImageState), deprecateImageReplacement)
	if err == nil {
		err = pending.Wait()
	}
	if err != nil {
		plog.Fatalf("Changing deprecation state of image failed: %v\n", err)
	}
}
