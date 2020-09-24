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
	"time"

	"github.com/spf13/cobra"
)

var (
	cmdGC = &cobra.Command{
		Use:   "gc",
		Short: "GC resources in AWS",
		Long:  `Delete instances created over the given duration ago`,
		RunE:  runGC,

		SilenceUsage: true,
	}

	gcDuration time.Duration
)

func init() {
	AWS.AddCommand(cmdGC)
	cmdGC.Flags().DurationVar(&gcDuration, "duration", 5*time.Hour, "how old resources must be before they're considered garbage")
}

func runGC(cmd *cobra.Command, args []string) error {
	err := API.GC(gcDuration)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Couldn't gc: %v\n", err)
		os.Exit(1)
	}
	return nil
}
