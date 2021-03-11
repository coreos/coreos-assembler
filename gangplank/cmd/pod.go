package main

import (
	"os"
	"os/exec"
	"strings"

	"github.com/coreos/gangplank/ocp"
	jobspec "github.com/coreos/gangplank/spec"
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

	// cosaWorkDirContext when true, ensures that the selinux system_u:object_r:container_file_t:s0
	// is set for the working directory.
	cosaWorkDirContext bool

	// cosaSrvDir is used as the scratch directory builds.
	cosaSrvDir string

	// automaticBuildStages is used to create automatic build stages
	automaticBuildStages []string

	// repoURLs are URLs to install from
	repoURLS []string
)

func init() {
	r, ok := os.LookupEnv("COSA_YUM_REPOS")
	if ok {
		repoURLS = strings.Split(r, ",")
	}

	cmdRoot.AddCommand(cmdPod)
	cmdPod.Flags().BoolVar(&cosaWorkDirContext, "setWorkDirCtx", false, "set workDir's selinux content")
	cmdPod.Flags().BoolVarP(&cosaViaPodman, "podman", "", false, "use podman to execute task")
	cmdPod.Flags().StringSliceVarP(&cosaCmds, "cmd", "c", []string{}, "commands to run")
	cmdPod.Flags().StringVarP(&cosaOverrideImage, "image", "i", "", "use an alternative image")
	cmdPod.Flags().StringVarP(&cosaSrvDir, "srvDir", "S", "", "podman mode - directory to mount as /srv")
	cmdPod.Flags().StringVarP(&cosaWorkDir, "workDir", "w", "", "podman mode - workdir to use")
	cmdPod.Flags().StringVarP(&serviceAccount, "serviceaccount", "a", "", "service account to use")
	cmdPod.Flags().StringSliceVar(&repoURLS, "repo", repoURLS, "yum repos to include for base builds")
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

	if specFile != "" {
		log.WithField("jobspec", specFile).Infof("Using jobspec from file")
	} else {
		log.Info("Generating jobspec from CLI arguments")
		generateJobSpec()
	}

	for _, r := range repoURLS {
		if r != "" {
			spec.Recipe.Repos = append(
				spec.Recipe.Repos,
				&jobspec.Repo{
					URL: r,
				})
		}
	}

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

	pb, err := ocp.NewPodBuilder(clusterCtx, cosaOverrideImage, serviceAccount, cosaWorkDir, &spec)
	if err != nil {
		log.Fatalf("failed to define builder pod: %v", err)
	}

	if err := pb.Exec(clusterCtx); err != nil {
		log.Fatalf("failed to execute CI builder: %v", err)
	}
}
