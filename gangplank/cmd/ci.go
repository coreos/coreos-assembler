package main

import (
	"github.com/coreos/gangplank/ocp"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	cmdCI = &cobra.Command{
		Use:   "ci",
		Short: "Execute COSA commands as part of a CI pipeline",
		Run:   runCI,
	}

	// cosaOverrideImage uses a different image
	cosaOverrideImage string

	// serviceAccount is the service acount to use for pod creation
	// and reading of the secrets.
	serviceAccount string
)

func init() {
	cmdRoot.AddCommand(cmdCI)
	cmdRoot.PersistentFlags().StringVarP(&cosaOverrideImage, "image", "i", "", "use an alternative image")
	cmdRoot.PersistentFlags().StringVarP(&serviceAccount, "serviceaccount", "a", "", "service account to use")
}

// runCI is the Jenkins/CI interface into entrypoint. It "mocks"
// the OpenShift buildconfig API with just-enough information to be
// useful.
func runCI(c *cobra.Command, args []string) {
	defer cancel()
	m, err := ocp.NewCIBuilder(ctx, cosaOverrideImage, serviceAccount, specFile)
	if err != nil {
		log.Fatalf("failed to define CI builder: %v", err)
	}

	log.Info("Starting entrypoint in CI Mode")
	if err := m.Exec(ctx); err != nil {
		log.Fatalf("failed to execute CI builder: %v", err)
	}
}
