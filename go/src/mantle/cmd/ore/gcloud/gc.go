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
	"time"

	"github.com/spf13/cobra"
)

var (
	cmdGC = &cobra.Command{
		Use:   "gc",
		Short: "GC resources in GCE",
		Long:  `Delete instances created over the given duration ago.`,
		RunE:  runGC,

		SilenceUsage: true,
	}

	gcDuration time.Duration
)

func init() {
	GCloud.AddCommand(cmdGC)
	cmdGC.Flags().DurationVar(&gcDuration, "duration", 5*time.Hour, "how old resources must be before they're considered garbage")
}

func runGC(cmd *cobra.Command, args []string) error {
	if len(args) != 0 {
		fmt.Fprintf(os.Stderr, "Unrecognized args in gcloud gc cmd: %v\n", args)
		os.Exit(2)
	}

	if err := api.GC(gcDuration); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	return nil
}
