package main

/*
	Definition for the main entry command. This defined the "human"
	interfaces for `run` and `run-steps`
*/

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"

	ee "github.com/coreos/entrypoint/exec"
	rhjobspec "github.com/coreos/entrypoint/spec"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const cosaContainerDir = "/usr/lib/coreos-assembler"

var (
	version = "devel"

	// cosaDir is the installed location of COSA. This defaults to
	// cosaContainerDir and is set via `-ldflags` at build time.
	cosaDir string

	// spec is an RHCOS spec. It is anticipated that this will be
	// changed in the future.
	spec     rhjobspec.JobSpec
	specFile string

	// entryEnvars are set for command execution
	entryEnvVars []string

	// shellCmd is the default command to execute commands.
	shellCmd = []string{"/bin/bash", "-x"}

	cmdRoot = &cobra.Command{
		Use:   "entry [command]",
		Short: "COSA entrypoint",
		Long: `Entrypoint for CoreOS Assemlber
Wrapper for COSA commands and templates`,
		PersistentPreRun: preRun,
	}

	cmdVersion = &cobra.Command{
		Use:   "version",
		Short: "Print the version number and exit.",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Printf("entry/%s version %s\n",
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

	entryEnvVars = os.Environ()

	log.SetOutput(os.Stdout)
	log.SetLevel(log.DebugLevel)
	newPath := fmt.Sprintf("%s:%s", cosaDir, os.Getenv("PATH"))
	os.Setenv("PATH", newPath)

	// cmdRoot options
	cmdRoot.PersistentFlags().StringVarP(&specFile, "spec", "s", "", "location of the spec")
	cmdRoot.AddCommand(cmdVersion)
	cmdRoot.AddCommand(cmdSingle)

	// cmdStep options
	cmdRoot.AddCommand(cmdSteps)
	cmdSteps.Flags().StringVarP(&specFile, "spec", "s", "", "location of the spec")
	cmdSteps.Flags().StringArrayVarP(&shellCmd, "shell", "S", shellCmd, "shellcommand to execute")
}

func main() {
	log.Infof("CoreOS-Assembler Entrypoint, %s", version)
	if err := cmdRoot.Execute(); err != nil {
		log.Fatal(err)
	}
	os.Exit(0)
}

// runScripts reads ARGs as files and executes the rendered templates.
func runScripts(c *cobra.Command, args []string) error {
	rendered := make(map[string]*os.File)
	for _, v := range args {
		in, err := ioutil.ReadFile(v)
		if err != nil {
			return err
		}
		t, err := ioutil.TempFile("", "rendered")
		if err != nil {
			return err
		}
		defer os.Remove(t.Name())
		rendered[v] = t

		tmpl, err := template.New("args").Parse(string(in))
		if err != nil {
			return fmt.Errorf("Failed to parse %s", err)
		}

		err = tmpl.Execute(t, spec)
		if err != nil {
			return fmt.Errorf("Failed render template: %v", err)
		}
	}

	log.Infof("Executing %d script(s)", len(rendered))
	for i, v := range rendered {
		log.WithFields(log.Fields{"script": i}).Info("Startig script")
		cArgs := append(shellCmd, v.Name())
		cmd := exec.Command(cArgs[0], cArgs[1:]...)
		cmd.Env = entryEnvVars
		rc, err := ee.RunCmds(cmd)
		if rc != 0 {
			return fmt.Errorf("Script exited with return code %d", rc)
		}
		if err != nil {
			return err
		}
		log.WithFields(log.Fields{"script": i}).Info("Script complete")
	}
	log.Infof("Execution complete")
	return nil
}

// runSingle renders args as templates and executes the command.
func runSingle(c *cobra.Command, args []string) {
	for i, v := range args {
		tmpl, err := template.New("args").Parse(v)
		if err != nil {
			log.WithFields(log.Fields{"input": v}).Fatalf("Failed to parse template")
		}

		var out bytes.Buffer
		err = tmpl.Execute(&out, spec)
		if err != nil {
			log.WithFields(log.Fields{"error": err}).Fatal("Failed to render template")
		}
		args[i] = out.String()
	}

	log.Infof("Executing commands: %v", args)
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = entryEnvVars
	rc, err := ee.RunCmds(cmd)
	if rc != 0 || err != nil {
		log.WithFields(log.Fields{
			"return code": rc,
			"error":       err,
			"command":     args,
		}).Error("Failed")
		os.Exit(rc)
	}
	log.Infof("Done")
}

// preRun processes the spec file.
func preRun(c *cobra.Command, args []string) {
	if specFile == "" {
		return
	}

	ns, err := rhjobspec.JobSpecFromFile(specFile)
	if err != nil {
		log.WithFields(log.Fields{"input file": specFile, "error": err}).Fatal(
			"Failed reading file")
	}
	spec = *ns
}
