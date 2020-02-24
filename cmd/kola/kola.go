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
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/coreos/pkg/capnslog"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/coreos/mantle/cli"
	"github.com/coreos/mantle/cosa"
	"github.com/coreos/mantle/fcos"
	"github.com/coreos/mantle/kola"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/sdk"
	"github.com/coreos/mantle/util"

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
		Use:   "run [glob pattern...]",
		Short: "Run kola tests by category",
		Long: `Run all kola tests (default) or related groups.

If the glob pattern is exactly equal to the name of a single test, any
restrictions on the versions of Container Linux supported by that test
will be ignored.
`,
		Run:    runRun,
		PreRun: preRun,
	}

	cmdRunUpgrade = &cobra.Command{
		Use:          "run-upgrade [glob pattern...]",
		Short:        "Run upgrade kola tests",
		Long:         `Run all upgrade kola tests (default) or related groups.`,
		RunE:         runRunUpgrade,
		PreRunE:      preRunUpgrade,
		PostRun:      postRunUpgrade,
		SilenceUsage: true,
	}

	cmdList = &cobra.Command{
		Use:   "list",
		Short: "List kola test names",
		Run:   runList,
	}

	cmdHttpServer = &cobra.Command{
		Use:   "http-server",
		Short: "Run a static webserver",
		Long: `Run a simple static webserver

This can be useful for e.g. serving locally built OSTree repos to qemu.
`,
		Run: runHttpServer,
	}

	listJSON           bool
	httpPort           int
	findParentImage    bool
	qemuImageDir       string
	qemuImageDirIsTemp bool
)

func init() {
	root.AddCommand(cmdRun)

	root.AddCommand(cmdList)
	cmdList.Flags().BoolVar(&listJSON, "json", false, "format output in JSON")

	root.AddCommand(cmdHttpServer)
	cmdHttpServer.Flags().IntVarP(&httpPort, "port", "P", 8000, "Listen on provided port")

	root.AddCommand(cmdRunUpgrade)
	cmdRunUpgrade.Flags().BoolVar(&findParentImage, "find-parent-image", false, "automatically find parent image if not provided -- note on qemu, this will download the image")
	cmdRunUpgrade.Flags().StringVar(&qemuImageDir, "qemu-image-dir", "", "directory in which to cache QEMU images if --fetch-parent-image is enabled")
}

func main() {
	cli.Execute(root)
}

func preRun(cmd *cobra.Command, args []string) {
	err := syncOptions()
	if err == nil {
		err = syncCosaOptions()
	}

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
	var patterns []string
	if len(args) == 0 {
		patterns = []string{"*"} // run all tests by default
	} else {
		patterns = args
	}

	var err error
	outputDir, err = kola.SetupOutputDir(outputDir, kolaPlatform)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	runErr := kola.RunTests(patterns, kolaPlatform, outputDir, !kola.Options.NoTestExitError)

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
		Image     string `json:"image"`
		ImageSize string `json:"imageSize"`
		Swtpm     bool   `json:"swtpm"`
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
			Image:     kola.QEMUOptions.DiskImage,
			ImageSize: kola.QEMUOptions.DiskSize,
			Swtpm:     kola.QEMUOptions.Swtpm,
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
			test.ExcludeArchitectures,
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
	Name                 string
	Platforms            []string
	ExcludePlatforms     []string `json:"-"`
	Architectures        []string
	ExcludeArchitectures []string `json:"-"`
	Distros              []string
	ExcludeDistros       []string `json:"-"`
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

func runHttpServer(cmd *cobra.Command, args []string) {
	directory := "."

	http.Handle("/", http.FileServer(http.Dir(directory)))

	fmt.Fprintf(os.Stdout, "Serving HTTP on port: %d\n", httpPort)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", httpPort), nil))
}

func preRunUpgrade(cmd *cobra.Command, args []string) error {
	// unlike `kola run`, we *require* the --cosa-build arg -- XXX: figure out
	// how to get this working using cobra's MarkFlagRequired()
	if kola.Options.CosaBuild == "" {
		errors.New("Error: missing required argument --cosa-build")
	}

	err := syncOptions()
	if err != nil {
		return err
	}

	if findParentImage {
		err = syncFindParentImageOptions()
		if err != nil {
			return err
		}
	}

	return nil
}

func postRunUpgrade(cmd *cobra.Command, args []string) {
	if qemuImageDir != "" && qemuImageDirIsTemp {
		os.RemoveAll(qemuImageDir)
	}
}

// syncFindParentImageOptions handles --find-parent-image automagic.
func syncFindParentImageOptions() error {
	var err error

	var parentBaseUrl string
	switch kola.Options.Distribution {
	case "fcos":
		parentBaseUrl, err = getParentFcosBuildBase()
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("--find-parent-image not yet supported for distro %s", kola.Options.Distribution)
	}

	var parentCosaBuild *cosa.Build
	parentCosaBuild, err = cosa.FetchAndParseBuild(parentBaseUrl + "meta.json")
	if err != nil {
		return err
	}

	// Here we handle the --fetch-parent-image --> platform-specific options
	// based on its cosa build metadata
	switch kolaPlatform {
	case "qemu-unpriv":
		if qemuImageDir == "" {
			if qemuImageDir, err = ioutil.TempDir("", "kola-run-upgrade"); err != nil {
				return err
			}
			qemuImageDirIsTemp = true
		}
		qcowUrl := parentBaseUrl + parentCosaBuild.BuildArtifacts.Qemu.Path
		qcowLocal := filepath.Join(qemuImageDir, parentCosaBuild.BuildArtifacts.Qemu.Path)
		decompressedQcowLocal, err := downloadImageAndDecompress(qcowUrl, qcowLocal)
		if err != nil {
			return err
		}
		kola.QEMUOptions.DiskImage = decompressedQcowLocal
	case "aws":
		kola.AWSOptions.AMI, err = parentCosaBuild.FindAMI(kola.AWSOptions.Region)
		if err != nil {
			return err
		}
	default:
		err = fmt.Errorf("--find-parent-image not yet supported for platform %s", kolaPlatform)
	}

	return nil
}

// Note this is a no-op if the decompressed dest already exists.
func downloadImageAndDecompress(url, compressedDest string) (string, error) {
	var decompressedDest string
	if strings.HasSuffix(compressedDest, ".xz") {
		// if the decompressed file is already present locally, assume it's
		// good and verified already
		decompressedDest = strings.TrimSuffix(compressedDest, ".xz")
		if exists, err := util.PathExists(decompressedDest); err != nil {
			return "", err
		} else if exists {
			return decompressedDest, nil
		} else {
			if err := sdk.DownloadCompressedSignedFile(decompressedDest, url, nil, "", util.XzDecompressStream); err != nil {
				return "", err
			}
			return decompressedDest, nil
		}
	}

	if err := sdk.DownloadSignedFile(compressedDest, url, nil, ""); err != nil {
		return "", err
	}

	return compressedDest, nil
}

func getParentFcosBuildBase() (string, error) {
	// For FCOS, we can be clever and automagically fetch the metadata for the
	// parent release, which should be the latest release on that stream.

	// We're taking liberal shortcuts here... the cleaner way to do this is
	// parse commitmeta.json for `fedora-coreos.stream`, then fetch the stream
	// metadata for that stream, then fetch the release metadata

	if kola.CosaBuild.BuildRef == "" {
		return "", errors.New("no ref in build metadata")
	}

	stream := filepath.Base(kola.CosaBuild.BuildRef)

	var parentVersion string
	if kola.CosaBuild.FedoraCoreOsParentVersion != "" {
		parentVersion = kola.CosaBuild.FedoraCoreOsParentVersion
	} else {
		// ok, we're probably operating on a local dev build since the pipeline
		// always injects the parent; just instead fetch the release index
		// for that stream and get the last build id from there
		index, err := fcos.FetchAndParseCanonicalReleaseIndex(stream)
		if err != nil {
			return "", err
		}

		n := len(index.Releases)
		if n == 0 {
			return "", fmt.Errorf("no parent version in build metadata, and no build on stream %s", stream)
		}

		parentVersion = index.Releases[n-1].Version
	}

	// XXX: multi-arch
	// XXX: centralize URL and parameterize
	return fmt.Sprintf("https://builds.coreos.fedoraproject.org/prod/streams/%s/builds/%s/x86_64/", stream, parentVersion), nil
}

func runRunUpgrade(cmd *cobra.Command, args []string) error {
	outputDir, err := kola.SetupOutputDir(outputDir, kolaPlatform)
	if err != nil {
		return err
	}

	var patterns []string
	if len(args) == 0 {
		patterns = []string{"*"} // run all tests by default
	} else {
		patterns = args
	}

	runErr := kola.RunUpgradeTests(patterns, kolaPlatform, outputDir, !kola.Options.NoTestExitError)

	// needs to be after RunTests() because harness empties the directory
	if err := writeProps(); err != nil {
		return err
	}

	return runErr
}
