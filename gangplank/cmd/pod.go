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

	// cosaWorkDir is used for podman mode and is where the "builds" directory will live
	cosaWorkDir string

	// cosaSrvDir is used as the scratch directory builds.
	cosaSrvDir string

	// cosaSrcDir is a source directory that is tarballed to be used by unbounded pods
	cosaSrcDir string
)

func init() {
	cmdRoot.AddCommand(cmdPod)
	cmdPod.Flags().BoolVarP(&cosaViaPodman, "podman", "", false, "use podman to execute task")
	cmdPod.Flags().StringSliceVarP(&cosaCmds, "cmd", "c", []string{}, "commands to run")
	cmdPod.Flags().StringVarP(&cosaOverrideImage, "image", "i", "", "use an alternative image")
	cmdPod.Flags().StringVarP(&cosaWorkDir, "workDir", "w", "", "podman mode - workdir to use")
	cmdPod.Flags().StringVarP(&cosaSrvDir, "srvDir", "S", "", "podman mode - directory to mount as /srv")
	cmdPod.Flags().StringVarP(&cosaSrcDir, "cosaSrcDir", "d", "", "directory to tarball to use as build source in /srv")
	cmdPod.Flags().StringVarP(&serviceAccount, "serviceaccount", "a", "", "service account to use")
}

// runPod is the Jenkins/CI interface into Gangplank. It "mocks"
// the OpenShift buildconfig API with just-enough information to be
// useful.
func runPod(c *cobra.Command, args []string) {
	defer cancel()

	cluster := ocp.NewCluster(true, "")

	if cosaViaPodman {
		cluster = ocp.NewCluster(false, "")
		cluster.SetPodman(cosaSrvDir)
		if cosaOverrideImage == "" {
			cosaOverrideImage = cosaDefaultImage
		}
		if cosaWorkDir == "" {
			cosaWorkDir, _ = os.Getwd()
		}
	}

	clusterCtx := ocp.NewClusterContext(ctx, cluster)

	pb, err := ocp.NewPodBuilder(clusterCtx, cosaOverrideImage, serviceAccount, specFile, cosaWorkDir, cosaSrcDir)
	if err != nil {
		log.Fatalf("failed to define builder pod: %v", err)
	}

	if err := pb.Exec(clusterCtx); err != nil {
		log.Fatalf("failed to execute CI builder: %v", err)
	}
}
