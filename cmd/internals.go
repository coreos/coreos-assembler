package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/coreos/coreos-assembler/pkg/pipeline"
)

func runInternals(argv []string) error {
	cmdInternals := &cobra.Command{
		Use:   "internals",
		Short: "Internal APIs of coreos-assembler",
	}

	cmdGetPipelineJson := &cobra.Command{
		Use:   "get-pipeline-json",
		Short: "Output pipeline config JSON",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			config, err := pipeline.ReadConfig(".")
			if err != nil {
				return err
			}
			buf, err := json.Marshal(config)
			if err != nil {
				return err
			}
			if _, err := fmt.Printf("%s\n", string(buf)); err != nil {
				return err
			}
			return nil
		},
	}
	cmdInternals.AddCommand(cmdGetPipelineJson)

	cmdInternals.SetArgs(argv)
	cmdInternals.Execute()

	return nil
}
