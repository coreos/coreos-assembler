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

package aliyun

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var (
	cmdTagImage = &cobra.Command{
		Use:   "tag-image --id <id> --tags foo=bar ...",
		Short: "Tag an image",
		Run:   runTagImage,
	}
	id     string
	tags   []string
	region string
)

func init() {
	// Initialize the command and its flags
	Aliyun.AddCommand(cmdTagImage)
	cmdTagImage.Flags().StringVar(&id, "id", "", "Aliyun Image ID")
	cmdTagImage.Flags().StringVar(&region, "region", "", "Region")
	cmdTagImage.Flags().StringSliceVar(&tags, "tags", []string{}, "list of key=value tags to attach to the Aliyun image")
}

func runTagImage(cmd *cobra.Command, args []string) {
	if id == "" {
		fmt.Fprintf(os.Stderr, "Provide --id to tag\n")
		os.Exit(1)
	}

	if region == "" {
		fmt.Fprintf(os.Stderr, "Provide --region\n")
		os.Exit(1)
	}

	tagMap := make(map[string]string)
	for _, tag := range tags {
		splitTag := strings.SplitN(tag, "=", 2)
		if len(splitTag) != 2 {
			fmt.Fprintf(os.Stderr, "invalid tag format; should be key=value, not %v\n", tag)
			os.Exit(1)
		}
		key, value := splitTag[0], splitTag[1]
		tagMap[key] = value
	}

	err := API.CreateTags(id, tagMap, region)
	if err != nil {
		fmt.Fprintf(os.Stderr, "couldn't create image tags: %v", err)
		os.Exit(1)
	}
}
