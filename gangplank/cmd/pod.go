package main

import (
	"os"

	"github.com/coreos/gangplank/ocp"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const cosaDefaultImage = "quay.io/coreos-assembler/coreos-assembler:latest"

var (
	cmdPod = &cobra.Command{
		Use:   "pod",
		Short: "Execute COSA command in an OpenShift Cluster (default) or Podman",
		Run:   runPod,
	}

	// cosaOverrideImage uses a different image
	cosaOverrideImage string

	// serviceAccount is the service acount to use for pod creation
	// and reading of the secrets.
	serviceAccount string

	// cosaCmds is used to define the commands to run
	cosaCmds []string

	// Run CI pod via podman (out of cluster)
	cosaViaPodman bool

	// workDir is used for podman mode
	cosaWorkDir string
)

func init() {
	cmdRoot.AddCommand(cmdPod)
	cmdPod.Flags().BoolVarP(&cosaViaPodman, "podman", "", false, "use podman to execute task")
	cmdPod.Flags().StringSliceVarP(&cosaCmds, "cmd", "c", []string{}, "commands to run")
	cmdPod.Flags().StringVarP(&cosaOverrideImage, "image", "i", "", "use an alternative image")
	cmdPod.Flags().StringVarP(&cosaWorkDir, "workDir", "w", "", "podman mode - workdir to use")
	cmdPod.Flags().StringVarP(&serviceAccount, "serviceaccount", "a", "", "service account to use")
}

// runPod is the Jenkins/CI interface into Gangplank. It "mocks"
// the OpenShift buildconfig API with just-enough information to be
// useful.
func runPod(c *cobra.Command, args []string) {
	defer cancel()

	inCluster := true
	launchMode := "OpenShift"
	if cosaViaPodman {
		inCluster = false
		launchMode = "Podman"
		if cosaOverrideImage == "" {
			cosaOverrideImage = cosaDefaultImage
		}
		if cosaWorkDir == "" {
			cosaWorkDir, _ = os.Getwd()
		}
	}
	pb, err := ocp.NewPodBuilder(ctx, inCluster, cosaOverrideImage, serviceAccount, specFile, cosaWorkDir)
	if err != nil {
		log.Fatalf("failed to define builder pod: %v", err)
	}

	log.Infof("Lauching %s worker pod(s)", launchMode)
	if err := pb.Exec(ctx, cosaWorkDir); err != nil {
		log.Fatalf("failed to execute CI builder: %v", err)
	}
}
