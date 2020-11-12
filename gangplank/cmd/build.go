package main

/*
	Command interface for OpenShift.

	"builder" is the sub-command that should be used as an
	the container entrypoint, i.e.:
		/usr/bin/dumbinit /usr/bin/gangplank builder


	Using the 'oc' binary was seriouslly considereed. However, the goals
	were to:
		1. Create and run concurrent pods
		2. Capture the logs
		3. Sanely clean-up after outselfs
	Using the API just made more sense.

*/

import (
	"context"
	"fmt"
	"time"

	"github.com/coreos/gangplank/ocp"
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

	// Terminal "keep alive" helper. When following logs via the `oc` commands,
	// cloud-deployed will send an EOF. To get around the EOF, the func sends a
	// null character that is not printed to the screen or reflected in the logs.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				fmt.Print("\x00")
				time.Sleep(1 * time.Second)
			}
		}
	}()

	b, err := ocp.NewBuilder(ctx)
	if err != nil {
		log.Fatal("Failed to find the build environment.")
	}

	if err := b.Exec(ctx); err != nil {
		log.WithFields(log.Fields{
			"err": err,
		}).Fatal("Failed to prepare environment.")
	}

}
