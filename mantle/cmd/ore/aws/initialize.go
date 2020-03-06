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

package aws

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	cmdInitialize = &cobra.Command{
		Use:   "initialize",
		Short: "initialize any uncreated resources for a given AWS region",
		RunE:  runInitialize,

		SilenceUsage: true,
	}

	bucket string
)

func init() {
	AWS.AddCommand(cmdInitialize)
	cmdInitialize.Flags().StringVar(&bucket, "bucket", "", "the S3 bucket URI to initialize; will default to a regional bucket")
}

func runInitialize(cmd *cobra.Command, args []string) error {
	if bucket == "" {
		bucket = defaultBucketNameForRegion(region)
	}

	err := API.InitializeBucket(bucket)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not initialize bucket %v: %v\n", bucket, err)
		os.Exit(1)
	}

	err = API.CreateImportRole(bucket)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not create import role for %v: %v\n", bucket, err)
		os.Exit(1)
	}
	return nil
}
