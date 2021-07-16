package main

/*
	Definition for the main Gangplank command. This defined the "human"
	interfaces for `run` and `run-steps`
*/

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/coreos/coreos-assembler-schema/cosa"
	jobspec "github.com/coreos/gangplank/internal/spec"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const cosaContainerDir = "/usr/lib/coreos-assembler"

var (
	ctx, cancel = context.WithCancel(context.Background())

	version = "devel"

	// cosaDir is the installed location of COSA. This defaults to
	// cosaContainerDir and is set via `-ldflags` at build time.
	cosaDir string

	// spec is a job spec.
	spec     jobspec.JobSpec
	specFile string

	// envVars are set for command execution
	envVars []string

	cmdRoot = &cobra.Command{
		Use:   "gangplank [command]",
		Short: "COSA Gangplank",
		Long: `OpenShift COSA Job Runner
Wrapper for COSA commands and templates`,
		PersistentPreRun: preRun,
	}

	cmdVersion = &cobra.Command{
		Use:   "version",
		Short: "Print the version number and exit.",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Printf("gangplank/%s version %s\n",
				cmd.Root().Name(), version)
		},
	}

	cmdSingle = &cobra.Command{
		Use:   "run",
		Short: "Run the commands and bail",
		Args:  cobra.MinimumNArgs(1),
		Run:   runSingle,
	}

	cmdPodless = &cobra.Command{
		Use:   "podless",
		Short: "Run outisde of pod (via prow/ci)",
		RunE:  runSpecLocally,
	}
)

var (
	// cosaInit indicates that cosa init should be run
	cosaInit bool

	// buildArch indicates the target architecture to build
	buildArch = cosa.BuilderArch()
)

func init() {
	if cosaDir == "" {
		path, err := os.Getwd()
		if err != nil {
			cosaDir = cosaContainerDir
		} else {
			cosaDir = filepath.Dir(path)
		}
	}

	envVars = os.Environ()

	log.SetOutput(os.Stdout)
	log.SetLevel(log.DebugLevel)
	newPath := fmt.Sprintf("%s:%s", cosaDir, os.Getenv("PATH"))
	os.Setenv("PATH", newPath)

	// cmdRoot options
	cmdRoot.PersistentFlags().StringVarP(&buildArch, "arch", "a", buildArch, "override the build arch")
	cmdRoot.PersistentFlags().StringVarP(&specFile, "spec", "s", "", "location of the spec")
	cmdRoot.AddCommand(cmdVersion)
	cmdRoot.AddCommand(cmdSingle)
	cmdRoot.Flags().StringVarP(&specFile, "spec", "s", "", "location of the spec")
	spec.AddCliFlags(cmdRoot.PersistentFlags())

	// cmdPodless options
	cmdRoot.AddCommand(cmdPodless)
	cmdPodless.Flags().AddFlagSet(specCommonFlags)
	cmdPodless.Flags().BoolVar(&cosaInit, "init", false, "force initialize srv dir")
}

func main() {
	log.Infof("Gangplank: COSA OpenShift job runner, %s", version)
	if err := cmdRoot.Execute(); err != nil {
		log.Fatal(err)
	}
	os.Exit(0)
}

// runSpecLocally executes a jobspec locally.
func runSpecLocally(c *cobra.Command, args []string) error {
	myDir, _ := os.Getwd()
	defer func() {
		ctx.Done()
		_ = os.Chdir(myDir)
	}()

	if _, err := os.Stat("src"); err != nil || cosaInit {
		log.WithField("dir", cosaSrvDir).Info("Initalizing Build Tree")
		spec.Stages = append(spec.Stages, jobspec.Stage{
			ID:             "Initialization",
			ExecutionOrder: 0,
			Commands: []string{
				"cosa init --force --branch {{ .JobSpec.Recipe.GitRef }} --force {{ .JobSpec.Recipe.GitURL}} ",
			},
		})
	}

	setCliSpec()
	rd := &jobspec.RenderData{
		JobSpec: &spec,
	}

	if err := os.Chdir(cosaSrvDir); err != nil {
		log.WithError(err).Fatal("failed to change to srv dir")
	}
	for _, stage := range spec.Stages {
		if err := stage.Execute(ctx, rd, envVars); err != nil {
			log.WithError(err).Fatal("failed to execute job")
		}
	}

	log.Infof("Execution complete")
	return nil
}

// runSingle renders args as templates and executes the command.
func runSingle(c *cobra.Command, args []string) {
	rd := &jobspec.RenderData{
		JobSpec: &spec,
	}
	x, err := rd.ExecuteTemplateFromString(args...)
	if err != nil {
		log.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, x[0], x[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}
	log.Infof("Done")
}

// preRun processes the spec file.
func preRun(c *cobra.Command, args []string) {
	// Set the build arch from the commandline
	if buildArch != cosa.BuilderArch() {
		cosa.SetArch(buildArch)
		log.WithField("arch", cosa.BuilderArch()).Info("Using non-native arch")
	}

	// Terminal "keep alive" helper. When following logs via the `oc` commands,
	// cloud-deployed instances will send an EOF. To get around the EOF, the func sends a
	// null character that is not printed to the screen or reflected in the logs.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(20 * time.Second):
				fmt.Print("\x00")
				time.Sleep(1 * time.Second)
			}
		}
	}()
}
