package main

/*
	Definition for the main Gangplank command. This defined the "human"
	interfaces for `run` and `run-steps`
*/

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	jobspec "github.com/coreos/gangplank/spec"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const cosaContainerDir = "/usr/lib/coreos-assembler"

var (
	version = "devel"

	// cosaDir is the installed location of COSA. This defaults to
	// cosaContainerDir and is set via `-ldflags` at build time.
	cosaDir string

	// spec is a job spec.
	spec     jobspec.JobSpec
	specFile string

	// envVars are set for command execution
	envVars []string

	// shellCmd is the default command to execute commands.
	shellCmd = []string{"/bin/bash", "-x"}

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

	cmdSteps = &cobra.Command{
		Use:          "run-scripts",
		Short:        "Run Steps from [file]",
		Args:         cobra.MinimumNArgs(1),
		RunE:         runScripts,
		SilenceUsage: true,
	}
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
	cmdRoot.PersistentFlags().StringVarP(&specFile, "spec", "s", "", "location of the spec")
	cmdRoot.AddCommand(cmdVersion)
	cmdRoot.AddCommand(cmdSingle)
	spec.AddCliFlags(cmdRoot.PersistentFlags())

	// cmdStep options
	cmdRoot.AddCommand(cmdSteps)
	cmdSteps.Flags().StringVarP(&specFile, "spec", "s", "", "location of the spec")
	cmdSteps.Flags().StringArrayVarP(&shellCmd, "shell", "S", shellCmd, "shellcommand to execute")
}

func main() {
	log.Infof("Gangplank: COSA OpenShift job runner, %s", version)
	if err := cmdRoot.Execute(); err != nil {
		log.Fatal(err)
	}
	os.Exit(0)
}

// runScripts reads ARGs as files and executes the rendered templates.
func runScripts(c *cobra.Command, args []string) error {
	rd := &jobspec.RenderData{
		JobSpec: &spec,
	}
	if err := rd.RendererExecuter(ctx, envVars, args...); err != nil {
		log.Fatalf("Failed to execute scripts: %v", err)
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
	if specFile == "" {
		return
	}

	ns, err := jobspec.JobSpecFromFile(specFile)
	if err != nil {
		log.WithFields(log.Fields{"input file": specFile, "error": err}).Fatal(
			"Failed reading file")
	}
	spec = ns
}
