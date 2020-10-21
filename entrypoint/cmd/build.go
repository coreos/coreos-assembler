package main

/*
	Command interface for OpenShift.

	"builder" is the sub-command that should be used as an
	the container entrypoint, i.e.:
		/usr/bin/dumbinit /usr/bin/entry builder
*/

import (
	"context"

	"github.com/coreos/entrypoint/ocp"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	cmdOCP = &cobra.Command{
		Use:   "builder",
		Short: "Execute as an OCP Builder",
		Run:   runOCP,
	}

	ctx, cancel = context.WithCancel(context.Background())
)

func init() {
	cmdRoot.AddCommand(cmdOCP)
}

// runOCP executes the Custom Build Strategy based on
// source or binary build strategies.
func runOCP(c *cobra.Command, args []string) {
	defer cancel()

	b, err := ocp.NewBuilder(ctx)
	if err != nil {
		log.Fatal("Failed to find the OCP build environment.")
	}

	if err := b.PrepareEnv(); err != nil {
		log.WithFields(log.Fields{
			"err": err,
		}).Fatal("Failed to prepare environment.")
	}

	if b.JobSpec != nil {
		spec = *b.JobSpec
		log.Info("Jobspec will apply to templated commands.")
	}

	if err := b.Exec(ctx); err != nil {
		log.WithFields(log.Fields{
			"err": err,
		}).Fatal("Failed to prepare environment.")
	}
}
