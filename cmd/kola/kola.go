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

	listJSON bool
)

func init() {
	root.AddCommand(cmdRun)
	root.AddCommand(cmdList)

	cmdList.Flags().BoolVar(&listJSON, "json", false, "format output in JSON")
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
	type Azure struct {
		DiskURI   string `json:"diskUri"`
		Publisher string `json:"publisher"`
		Offer     string `json:"offer"`
		Sku       string `json:"sku"`
		Version   string `json:"version"`
		Location  string `json:"location"`
		Size      string `json:"size"`
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
	type OpenStack struct {
		Region string `json:"region"`
		Image  string `json:"image"`
		Flavor string `json:"flavor"`
	}
	type Packet struct {
		Facility              string `json:"facility"`
		Plan                  string `json:"plan"`
		InstallerImageBaseURL string `json:"installer"`
		ImageURL              string `json:"image"`
	}
	type QEMU struct {
		Image string `json:"image"`
		Swtpm bool   `json:"swtpm"`
	}
	return enc.Encode(&struct {
		Cmdline         []string  `json:"cmdline"`
		Platform        string    `json:"platform"`
		Distro          string    `json:"distro"`
		IgnitionVersion string    `json:"ignitionversion"`
		Board           string    `json:"board"`
		OSContainer     string    `json:"oscontainer"`
		AWS             AWS       `json:"aws"`
		Azure           Azure     `json:"azure"`
		DO              DO        `json:"do"`
		ESX             ESX       `json:"esx"`
		GCE             GCE       `json:"gce"`
		OpenStack       OpenStack `json:"openstack"`
		Packet          Packet    `json:"packet"`
		QEMU            QEMU      `json:"qemu"`
	}{
		Cmdline:         os.Args,
		Platform:        kolaPlatform,
		Distro:          kola.Options.Distribution,
		IgnitionVersion: kola.Options.IgnitionVersion,
		Board:           kola.QEMUOptions.Board,
		OSContainer:     kola.Options.OSContainer,
		AWS: AWS{
			Region:       kola.AWSOptions.Region,
			AMI:          kola.AWSOptions.AMI,
			InstanceType: kola.AWSOptions.InstanceType,
		},
		Azure: Azure{
			DiskURI:   kola.AzureOptions.DiskURI,
			Publisher: kola.AzureOptions.Publisher,
			Offer:     kola.AzureOptions.Offer,
			Sku:       kola.AzureOptions.Sku,
			Version:   kola.AzureOptions.Version,
			Location:  kola.AzureOptions.Location,
			Size:      kola.AzureOptions.Size,
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
		OpenStack: OpenStack{
			Region: kola.OpenStackOptions.Region,
			Image:  kola.OpenStackOptions.Image,
			Flavor: kola.OpenStackOptions.Flavor,
		},
		Packet: Packet{
			Facility:              kola.PacketOptions.Facility,
			Plan:                  kola.PacketOptions.Plan,
			InstallerImageBaseURL: kola.PacketOptions.InstallerImageBaseURL,
			ImageURL:              kola.PacketOptions.ImageURL,
		},
		QEMU: QEMU{
			Image: kola.QEMUOptions.DiskImage,
			Swtpm: kola.QEMUOptions.Swtpm,
		},
	})
}

func runList(cmd *cobra.Command, args []string) {
	var testlist []*item
	for name, test := range register.Tests {
		item := &item{
			name,
			test.Platforms,
			test.ExcludePlatforms,
			test.Architectures,
			test.Distros,
			test.ExcludeDistros}
		item.updateValues()
		testlist = append(testlist, item)
	}

	sort.Slice(testlist, func(i, j int) bool {
		return testlist[i].Name < testlist[j].Name
	})

	if !listJSON {
		var w = tabwriter.NewWriter(os.Stdout, 0, 8, 0, '\t', 0)

		fmt.Fprintln(w, "Test Name\tPlatforms\tArchitectures\tDistributions")
		fmt.Fprintln(w, "\t")
		for _, item := range testlist {
			fmt.Fprintf(w, "%v\n", item)
		}
		w.Flush()
	} else {
		out, err := json.MarshalIndent(testlist, "", "\t")
		if err != nil {
			fmt.Fprintf(os.Stderr, "marshalling test list: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(string(out))
	}
}

type item struct {
	Name             string
	Platforms        []string
	ExcludePlatforms []string `json:"-"`
	Architectures    []string
	Distros          []string
	ExcludeDistros   []string `json:"-"`
}

func (i *item) updateValues() {
	buildItems := func(include, exclude, all []string) []string {
		if len(include) == 0 && len(exclude) == 0 {
			if listJSON {
				return all
			} else {
				return []string{"all"}
			}
		}
		var retItems []string
		if len(exclude) > 0 {
			excludeMap := map[string]struct{}{}
			for _, item := range exclude {
				excludeMap[item] = struct{}{}
			}
			if len(include) == 0 {
				retItems = all
			} else {
				retItems = include
			}
			items := []string{}
			for _, item := range retItems {
				if _, ok := excludeMap[item]; !ok {
					items = append(items, item)
				}
			}
			retItems = items
		} else {
			retItems = include
		}
		return retItems
	}
	i.Platforms = buildItems(i.Platforms, i.ExcludePlatforms, kolaPlatforms)
	i.Architectures = buildItems(i.Architectures, nil, kolaArchitectures)
	i.Distros = buildItems(i.Distros, i.ExcludeDistros, kolaDistros)
}

func (i item) String() string {
	return fmt.Sprintf("%v\t%v\t%v\t%v", i.Name, i.Platforms, i.Architectures, i.Distros)
}
