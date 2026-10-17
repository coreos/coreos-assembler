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

package gcloud

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var (
	cmdLabelImage = &cobra.Command{
		Use:   "label-image --name <name> --labels foo=bar ...",
		Short: "label an image",
		Run:   runLabelImage,
	}
	name   string
	labels []string
)

func init() {
	// Initialize the command and its flags
	GCloud.AddCommand(cmdLabelImage)
	cmdLabelImage.Flags().StringVar(&name, "name", "", "GCloud Image Name")
	cmdLabelImage.Flags().StringSliceVar(&labels, "labels", []string{}, "list of key=value labels to attach to the GCloud image")
}

func runLabelImage(cmd *cobra.Command, args []string) {
	if name == "" {
		fmt.Fprintf(os.Stderr, "Provide --name to label\n")
		os.Exit(1)
	}

	if len(labels) < 1 {
		fmt.Fprintf(os.Stderr, "Provide at least one --label to label\n")
		os.Exit(1)
	}

	labelMap := make(map[string]string)
	for _, label := range labels {
		splitLabel := strings.SplitN(label, "=", 2)
		if len(splitLabel) != 2 {
			fmt.Fprintf(os.Stderr, "invalid label format; should be key=value, not %v\n", label)
			os.Exit(1)
		}
		key, value := splitLabel[0], splitLabel[1]
		labelMap[key] = value
	}

	err := api.SetImageLabels(name, labelMap)
	if err != nil {
		fmt.Fprintf(os.Stderr, "couldn't create image labels: %v", err)
		os.Exit(1)
	}
}
