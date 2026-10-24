// Copyright The Mantle Authors.
// SPDX-License-Identifier: Apache-2.0

package stackit

import (
	"time"

	"github.com/spf13/cobra"
)

func init() {
	var gracePeriod time.Duration
	cmd := &cobra.Command{
		Use:          "gc",
		Short:        "Delete old STACKIT resources created by Mantle",
		Long:         "Delete resources created by this STACKIT provider in the selected project and region that are older than the grace period. Unmarked resources and resources still referenced by retained servers are preserved.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return api.GC(cmd.Context(), gracePeriod)
		},
	}
	cmd.Flags().DurationVar(&gracePeriod, "duration", 5*time.Hour, "Minimum age of resources to delete")
	STACKIT.AddCommand(cmd)
}
