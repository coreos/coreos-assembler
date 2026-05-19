// Copyright 2026 Red Hat, Inc.
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

package aws

import (
	"fmt"
	"os"
	"time"

	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/spf13/cobra"
)

var (
	cmdEnsurePublic = &cobra.Command{
		Use:   "ensure-public",
		Short: "Ensure production RHCOS AMIs remain publicly accessible",
		Long: `Restores production RHCOS AMIs (tagged production=true) that have gone
private due to AWS's automatic AMI deprecation policy.

AWS enforces a 2-year deprecation date on all public AMIs and removes their
public sharing permission after 6+ months of inactivity post-deprecation.
This breaks OpenShift customers trying to scale cluster nodes with older images.
DisableImageDeprecation has no effect on public AMIs; the only mitigation is
periodic detection and re-publication.

Exits non-zero if any AMI could not be restored.

Examples:

  # Restore any private production AMIs in a region
  ore aws ensure-public --region us-east-1

  # Restore a specific AMI by ID
  ore aws ensure-public --region us-east-1 --ami ami-0abc123`,
		RunE:         runEnsurePublic,
		SilenceUsage: true,
	}

	ensurePublicAMI string
)

func init() {
	AWS.AddCommand(cmdEnsurePublic)
	cmdEnsurePublic.Flags().StringVar(&ensurePublicAMI, "ami", "",
		"Target a single AMI by ID; bypasses the production=true tag filter.")
}

func runEnsurePublic(cmd *cobra.Command, args []string) error {
	var images []ec2types.Image

	// Fetch the target AMI directly, or list all production AMIs in the region.
	if ensurePublicAMI != "" {
		img, err := API.GetImageByID(ensurePublicAMI)
		if err != nil {
			return fmt.Errorf("fetching AMI %s: %v", ensurePublicAMI, err)
		}
		if img == nil {
			return fmt.Errorf("AMI %s not found in region %s", ensurePublicAMI, region)
		}
		images = []ec2types.Image{*img}
	} else {
		var err error
		images, err = API.ListProductionImages()
		if err != nil {
			return fmt.Errorf("listing production AMIs in %s: %v", region, err)
		}
	}

	// Process every AMI before returning so a single failure doesn't skip the rest.
	// Errors are collected and reported together at the end.
	hadError := false

	for _, img := range images {
		imgID := derefStr(img.ImageId)
		if imgID == "" {
			fmt.Fprintf(os.Stderr, "skipping image with nil ID\n")
			hadError = true
			continue
		}
		// Name is only used for logging so we don't skip if it's missing.
		name := derefStr(img.Name)

		isPublic, err := API.IsImagePublic(imgID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "checking permissions for %s: %v\n", imgID, err)
			hadError = true
			continue
		}
		if !isPublic {
			if err := API.RestoreImagePublic(imgID); err != nil {
				fmt.Fprintf(os.Stderr, "error restoring %s (%s): %v\n", imgID, name, err)
				hadError = true
				continue
			}
			// Print each AMI that was successfully restored to public.
			fmt.Printf("restored %s (%s) — %s\n", imgID, name, formatDeprecationTime(img.DeprecationTime))
		}
	}

	if hadError {
		return fmt.Errorf("one or more AMIs could not be restored to public")
	}
	return nil
}

// formatDeprecationTime parses an AWS deprecation timestamp and returns a
// short human-readable label, e.g. "deprecated on 2025-01-15".
func formatDeprecationTime(s *string) string {
	if s == nil || *s == "" {
		return "no deprecation date"
	}
	for _, layout := range []string{time.RFC3339, time.RFC3339Nano, "2006-01-02T15:04:05.000Z"} {
		if t, err := time.Parse(layout, *s); err == nil {
			return fmt.Sprintf("deprecated on %s", t.Format("2006-01-02"))
		}
	}
	return fmt.Sprintf("deprecated on %s", *s)
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
