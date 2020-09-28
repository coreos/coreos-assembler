package main

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"text/template"

	ee "github.com/coreos/entrypoint/exec"
	rhjobspec "github.com/coreos/entrypoint/spec"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
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
		Use:   "run-steps",
		Short: "Run Steps from [file]",
		Args:  cobra.MinimumNArgs(1),
		Run:   runSteps,
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

// runSteps reads ARGs as files and executes the rendered templates.
func runSteps(c *cobra.Command, args []string) {
	rendered := make(map[string]*os.File)
	for _, v := range args {
		in, err := ioutil.ReadFile(v)
		if err != nil {
			log.Fatal(err)
		}
		t, err := ioutil.TempFile("", "rendered")
		if err != nil {
			log.Fatal(err)
		}
		defer os.Remove(t.Name())
		rendered[v] = t

		tmpl, err := template.New("args").Parse(string(in))
		if err != nil {
			log.Fatal("failed to parse")
		}

		err = tmpl.Execute(t, spec)
		if err != nil {
			log.Fatal("failed render template", err)
		}
	}

	log.Infof("executing %d script(s)", len(rendered))
	for i, v := range rendered {
		log.Infof("executing script %q", i)

		rc, err := runCmds(append(shellCmd, v.Name()))
		if rc != 0 {
			log.WithFields(log.Fields{
				"script":      i,
				"return code": rc,
				"error":       err,
			}).Error("failed")
			os.Exit(rc)
		}
	}
	log.Info("done")
}

// runSingle renders args as templates and executes the command.
func runSingle(c *cobra.Command, args []string) {
	for i, v := range args {
		tmpl, err := template.New("args").Parse(v)
		if err != nil {
			log.WithFields(log.Fields{"input": v}).Fatalf("failed to parse template")
		}

		var out bytes.Buffer
		err = tmpl.Execute(&out, spec)
		if err != nil {
			log.WithFields(log.Fields{"error": err}).Fatal("failed to render template")
		}
		args[i] = out.String()
	}

	log.Infof("executing commands: %v", args)
	rc, err := runCmds(args)
	if rc != 0 || err != nil {
		log.WithFields(log.Fields{
			"return code": rc,
			"error":       err,
			"command":     args,
		}).Error("failed")
		os.Exit(rc)
	}
	log.Infof("Done")
}

// runCmds runs args as a command and ensures that each
func runCmds(args []string) (int, error) {
	if len(args) <= 1 {
		os.Exit(0)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go ee.RemoveZombies(ctx, &wg)

	var rc int
	cmd := exec.Command(args[0], args[1:]...)
	err := ee.Run(cmd)
	if err != nil {
		rc = 1
	}

	cancel()
	wg.Wait()
	return rc, err
}

// preRun processes the spec file.
func preRun(c *cobra.Command, args []string) {
	if specFile == "" {
		log.Debug("no spec configuration found")
		return
	}

	in, err := ioutil.ReadFile(specFile)
	if err != nil {
		log.WithFields(log.Fields{"input file": specFile, "error": err}).Fatal(
			"failed reading file")
	}

	if err = yaml.Unmarshal(in, &spec); err != nil {
		log.WithFields(log.Fields{"input file": specFile, "error": err}).Fatal(
			"failed unmarshalling yaml")
	}
}
