// Copyright 2015 CoreOS, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"text/tabwriter"

	"github.com/coreos/pkg/capnslog"
	"github.com/spf13/cobra"

	"github.com/coreos/mantle/cli"
	"github.com/coreos/mantle/kola"
	"github.com/coreos/mantle/kola/register"

	// register OS test suite
	_ "github.com/coreos/mantle/kola/registry"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "kola")

	root = &cobra.Command{
		Use:   "kola [command]",
		Short: "The CoreOS Superdeep Borehole",
		// http://en.wikipedia.org/wiki/Kola_Superdeep_Borehole
	}

	cmdRun = &cobra.Command{
		Use:   "run [glob pattern]",
		Short: "Run kola tests by category",
		Long: `Run all kola tests (default) or related groups.

If the glob pattern is exactly equal to the name of a single test, any
restrictions on the versions of Container Linux supported by that test
will be ignored.
`,
		Run:    runRun,
		PreRun: preRun,
	}

	cmdList = &cobra.Command{
		Use:   "list",
		Short: "List kola test names",
		Run:   runList,
	}
)

func init() {
	root.AddCommand(cmdRun)
	root.AddCommand(cmdList)
}

func main() {
	cli.Execute(root)
}

func preRun(cmd *cobra.Command, args []string) {
	err := syncOptions()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(3)
	}

	// Packet uses storage, and storage talks too much.
	if !plog.LevelAt(capnslog.INFO) {
		mantleLogger := capnslog.MustRepoLogger("github.com/coreos/mantle")
		mantleLogger.SetLogLevel(map[string]capnslog.LogLevel{
			"storage": capnslog.WARNING,
		})
	}
}

func runRun(cmd *cobra.Command, args []string) {
	if len(args) > 1 {
		fmt.Fprintf(os.Stderr, "Extra arguments specified. Usage: 'kola run [glob pattern]'\n")
		os.Exit(2)
	}
	var pattern string
	if len(args) == 1 {
		pattern = args[0]
	} else {
		pattern = "*" // run all tests by default
	}

	var err error
	outputDir, err = kola.SetupOutputDir(outputDir, kolaPlatform)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	runErr := kola.RunTests(pattern, kolaPlatform, outputDir)

	// needs to be after RunTests() because harness empties the directory
	if err := writeProps(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	if runErr != nil {
		fmt.Fprintf(os.Stderr, "%v\n", runErr)
		os.Exit(1)
	}
}

func writeProps() error {
	f, err := os.OpenFile(filepath.Join(outputDir, "properties.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "    ")

	type AWS struct {
		Region       string `json:"region"`
		AMI          string `json:"ami"`
		InstanceType string `json:"type"`
	}
	type DO struct {
		Region string `json:"region"`
		Size   string `json:"size"`
		Image  string `json:"image"`
	}
	type ESX struct {
		Server     string `json:"server"`
		BaseVMName string `json:"base_vm_name"`
	}
	type GCE struct {
		Image       string `json:"image"`
		MachineType string `json:"type"`
	}
	type Packet struct {
		Facility              string `json:"facility"`
		Plan                  string `json:"plan"`
		InstallerImageBaseURL string `json:"installer"`
		ImageURL              string `json:"image"`
	}
	type QEMU struct {
		Image string `json:"image"`
	}
	return enc.Encode(&struct {
		Cmdline  []string `json:"cmdline"`
		Platform string   `json:"platform"`
		Distro   string   `json:"distro"`
		Board    string   `json:"board"`
		AWS      AWS      `json:"aws"`
		DO       DO       `json:"do"`
		ESX      ESX      `json:"esx"`
		GCE      GCE      `json:"gce"`
		Packet   Packet   `json:"packet"`
		QEMU     QEMU     `json:"qemu"`
	}{
		Cmdline:  os.Args,
		Platform: kolaPlatform,
		Distro:   kola.Options.Distribution,
		Board:    kola.QEMUOptions.Board,
		AWS: AWS{
			Region:       kola.AWSOptions.Region,
			AMI:          kola.AWSOptions.AMI,
			InstanceType: kola.AWSOptions.InstanceType,
		},
		DO: DO{
			Region: kola.DOOptions.Region,
			Size:   kola.DOOptions.Size,
			Image:  kola.DOOptions.Image,
		},
		ESX: ESX{
			Server:     kola.ESXOptions.Server,
			BaseVMName: kola.ESXOptions.BaseVMName,
		},
		GCE: GCE{
			Image:       kola.GCEOptions.Image,
			MachineType: kola.GCEOptions.MachineType,
		},
		Packet: Packet{
			Facility: kola.PacketOptions.Facility,
			Plan:     kola.PacketOptions.Plan,
			InstallerImageBaseURL: kola.PacketOptions.InstallerImageBaseURL,
			ImageURL:              kola.PacketOptions.ImageURL,
		},
		QEMU: QEMU{
			Image: kola.QEMUOptions.DiskImage,
		},
	})
}

func runList(cmd *cobra.Command, args []string) {
	var w = tabwriter.NewWriter(os.Stdout, 0, 8, 0, '\t', 0)
	var testlist []item

	for name, test := range register.Tests {
		testlist = append(testlist, item{
			name,
			test.Platforms,
			test.ExcludePlatforms,
			test.Architectures})
	}

	sort.Slice(testlist, func(i, j int) bool {
		return testlist[i].Name < testlist[j].Name
	})

	fmt.Fprintln(w, "Test Name\tPlatforms\tArchitectures")
	fmt.Fprintln(w, "\t")
	for _, item := range testlist {
		fmt.Fprintf(w, "%v\n", item)
	}
	w.Flush()
}

type item struct {
	Name             string
	Platforms        []string
	ExcludePlatforms []string
	Architectures    []string
}

func (i item) String() string {
	if len(i.ExcludePlatforms) > 0 {
		excludePlatforms := map[string]struct{}{}
		for _, platform := range i.ExcludePlatforms {
			excludePlatforms[platform] = struct{}{}
		}
		if len(i.Platforms) == 0 {
			i.Platforms = kolaPlatforms
		}
		platforms := []string{}
		for _, platform := range i.Platforms {
			if _, ok := excludePlatforms[platform]; !ok {
				platforms = append(platforms, platform)
			}
		}
		i.Platforms = platforms
	}
	if len(i.Platforms) == 0 {
		i.Platforms = []string{"all"}
	}
	if len(i.Architectures) == 0 {
		i.Architectures = []string{"all"}
	}
	return fmt.Sprintf("%v\t%v\t%v", i.Name, i.Platforms, i.Architectures)
}
