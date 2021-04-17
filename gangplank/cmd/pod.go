package main

import (
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/coreos/gangplank/ocp"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	cosaDefaultImage        = "quay.io/coreos-assembler/coreos-assembler:latest"
	cosaWorkDirSelinuxLabel = "system_u:object_r:container_file_t:s0"
)

var (
	cmdPod = &cobra.Command{
		Use:   "pod",
		Short: "Execute COSA command in an OpenShift Cluster (default) or Podman",
		Run:   runPod,
	}

	// cosaImage uses a different image
	cosaImage string

	// serviceAccount is the service acount to use for pod creation
	// and reading of the secrets.
	serviceAccount string

	// Run CI pod via podman (out of cluster)
	cosaViaPodman bool

	// cosaWorkDir is used for podman mode and is where the "builds" directory will live
	cosaWorkDir string

	// cosaWorkDirContext when true, ensures that the selinux system_u:object_r:container_file_t:s0
	// is set for the working directory.
	cosaWorkDirContext bool

	// cosaSrvDir is used as the scratch directory builds.
	cosaSrvDir string

	// cosaNamespace when defined will launch the pod in another namespace
	cosaNamespace string

	// automaticBuildStages is used to create automatic build stages
	automaticBuildStages []string

	// remoteKubeConfig sets Gangplank in remote mode
	remoteKubeConfig string
)

func init() {
	cmdRoot.AddCommand(cmdPod)

	spec.AddCliFlags(cmdPod.Flags())
	cmdPod.Flags().BoolVar(&cosaWorkDirContext, "setWorkDirCtx", false, "set workDir's selinux content")
	cmdPod.Flags().BoolVarP(&cosaViaPodman, "podman", "", false, "use podman to execute task")
	cmdPod.Flags().StringVarP(&cosaImage, "image", "i", "", "use an alternative image")
	cmdPod.Flags().StringVarP(&cosaWorkDir, "workDir", "w", "", "podman mode - workdir to use")
	cmdPod.Flags().StringVarP(&serviceAccount, "serviceaccount", "a", "", "service account to use")

	cmdPod.Flags().StringVarP(&remoteKubeConfig, "remoteKubeConfig", "R", "", "launch COSA in a remote cluster")
	cmdPod.Flags().StringVarP(&cosaNamespace, "namespace", "N", "", "use a different namespace")
	cmdPod.Flags().AddFlagSet(specCommonFlags)
}

// runPod is the Jenkins/CI interface into Gangplank. It "mocks"
// the OpenShift buildconfig API with just-enough information to be
// useful.
func runPod(c *cobra.Command, args []string) {
	defer cancel()

	cluster := ocp.NewCluster(true)

	if cosaViaPodman {
		cluster = ocp.NewCluster(false)
		cluster.SetPodman(cosaSrvDir)
	}

	if cosaViaPodman || remoteKubeConfig != "" {
		if cosaWorkDir == "" {
			cosaWorkDir, _ = os.Getwd()
		}
		if cosaImage == "" {
			log.WithField("image", cosaDefaultImage).Info("Using default COSA image")
			cosaImage = cosaDefaultImage
		}
	}

	if remoteKubeConfig != "" {
		log.Info("Using a hop pod via a remote cluster")
		cluster.SetRemoteCluster(remoteKubeConfig, cosaNamespace)
		if cosaWorkDir == "" {
			cosaWorkDir, _ = os.Getwd()
		}
		log.Infof("Logs will written to %s/logs", cosaWorkDir)
	}

	clusterCtx := ocp.NewClusterContext(ctx, cluster)
	setCliSpec()
	spec.Job.MinioCfgFile = minioCfgFile

	if cosaWorkDirContext {
		for _, d := range []string{cosaWorkDir, cosaSrvDir} {
			if d == "" {
				continue
			}
			log.WithField("dir", d).Infof("Applying selinux %q content", cosaWorkDirSelinuxLabel)
			args := []string{"chcon", "-R", cosaWorkDirSelinuxLabel, d}
			cmd := exec.CommandContext(ctx, args[0], args[1:]...)
			if err := cmd.Run(); err != nil {
				log.WithError(err).Fatalf("failed set dir context on %s", d)
			}
		}
	}

	if remoteKubeConfig != "" {
		h := ocp.NewHopPod(clusterCtx, cosaImage, serviceAccount, cosaWorkDir, &spec)
		term := make(chan bool)

		sig := make(chan os.Signal, 256)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGHUP)
		go func() {
			<-sig
			term <- true
		}()
		defer func() {
			term <- true
		}()

		err := h.WorkerRunner(term, nil)
		if err != nil {
			log.WithError(err).Fatal("failed remote exection of a pod")
		}
	}

	// Run in cluster or podman mode
	pb, err := ocp.NewPodBuilder(clusterCtx, cosaImage, serviceAccount, cosaWorkDir, &spec)
	if err != nil {
		log.Fatalf("failed to define builder pod: %v", err)
	}

	if err := pb.Exec(clusterCtx); err != nil {
		log.Fatalf("failed to execute CI builder: %v", err)
	}
}
