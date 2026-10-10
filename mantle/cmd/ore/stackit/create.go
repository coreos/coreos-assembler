// Copyright The Mantle Authors.
// SPDX-License-Identifier: Apache-2.0

package stackit

import (
	"fmt"

	coreosarch "github.com/coreos/stream-metadata-go/arch"
	"github.com/spf13/cobra"
)

func init() {
	var path, name, arch string
	cmd := &cobra.Command{
		Use:          "create-image",
		Short:        "Upload a CoreOS QCOW2 image to STACKIT",
		Long:         "Upload a CoreOS QCOW2 image to STACKIT and print its image ID. The image must already support ignition.platform.id=stackit.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := api.UploadImage(cmd.Context(), name, path, arch)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), id)
			return err
		},
	}
	cmd.Flags().StringVar(&path, "file", "", "Path to an uncompressed QCOW2 image")
	cmd.Flags().StringVar(&name, "name", "", "Image name")
	cmd.Flags().StringVar(&arch, "arch", coreosarch.CurrentRpmArch(), "Image architecture: x86_64 or aarch64")
	cmd.MarkFlagRequired("file")
	cmd.MarkFlagRequired("name")
	STACKIT.AddCommand(cmd)
}
