// Copyright The Mantle Authors.
// SPDX-License-Identifier: Apache-2.0

package stackit

import (
	"fmt"

	"github.com/coreos/coreos-assembler/mantle/cli"
	"github.com/coreos/coreos-assembler/mantle/platform/api/stackit"
	"github.com/spf13/cobra"
)

var (
	STACKIT = &cobra.Command{
		Use:   "stackit [command]",
		Short: "STACKIT image and resource utilities",
	}
	api     *stackit.API
	options stackit.Options
)

func init() {
	flags := STACKIT.PersistentFlags()
	flags.StringVar(&options.Project, "stackit-project", "", "STACKIT project ID")
	flags.StringVar(&options.Region, "stackit-region", "eu01", "STACKIT region")
	flags.StringVar(&options.ServiceAccountKeyPath, "stackit-service-account-key-path", "", "STACKIT service account JSON key file (defaults to SDK credential discovery)")
	flags.StringVar(&options.TokenFile, "stackit-token-file", "", "File containing a raw STACKIT service account token")
	cli.WrapPreRun(STACKIT, func(cmd *cobra.Command, args []string) error {
		var err error
		api, err = stackit.New(&options)
		if err != nil {
			return fmt.Errorf("creating STACKIT client: %w", err)
		}
		return nil
	})
}
