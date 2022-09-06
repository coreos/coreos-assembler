// Copyright Red Hat, Inc.
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
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/coreos/stream-metadata-go/release"
	"github.com/coreos/stream-metadata-go/stream"
	"github.com/spf13/cobra"

	"github.com/coreos/mantle/version"
)

const (
	// This will hopefully migrate to mirror.openshift.com, see https://github.com/openshift/os/issues/477
	rhcosCosaEndpoint = "https://rhcos.mirror.openshift.com/art/storage/releases"
)

var (
	cmdCosaBuildToStream = &cobra.Command{
		Use:   "cosa2stream [options]",
		Short: "Generate stream JSON from a coreos-assembler build",
		RunE:  runCosaBuildToStream,

		SilenceUsage: true,
	}

	nosignatures  bool
	streamBaseURL string
	streamName    string
	distro        string
	target        string
)

func init() {
	cmdCosaBuildToStream.Flags().StringVar(&streamBaseURL, "url", "", "Base URL for build")
	cmdCosaBuildToStream.Flags().StringVar(&streamName, "name", "", "Stream name")
	cmdCosaBuildToStream.Flags().StringVar(&distro, "distro", "", "Distribution (fcos, rhcos)")
	cmdCosaBuildToStream.Flags().StringVar(&target, "target", "", "Modify this file in place (default: no source, print to stdout)")
	cmdCosaBuildToStream.Flags().BoolVar(&nosignatures, "no-signatures", false, "Omit signatures (useful to generate pre-release stream metadata)")
	root.AddCommand(cmdCosaBuildToStream)
}

func runCosaBuildToStream(cmd *cobra.Command, args []string) error {
	var outStream stream.Stream

	if target != "" {
		buf, err := ioutil.ReadFile(target)
		if err != nil {
			return err
		}
		err = json.Unmarshal(buf, &outStream)
		if err != nil {
			return err
		}
	} else {
		if streamName == "" {
			return fmt.Errorf("--name must be provided (if no input file)")
		}
		outStream = stream.Stream{
			Stream:        streamName,
			Architectures: make(map[string]stream.Arch),
		}
	}
	streamArches := outStream.Architectures

	outStream.Metadata = stream.Metadata{
		LastModified: time.Now().UTC().Format(time.RFC3339),
		Generator:    "plume cosa2stream " + version.Version,
	}

	childArgs := []string{"generate-release-meta"}
	if distro != "" {
		childArgs = append(childArgs, "--distro="+distro)
	}
	if streamBaseURL != "" {
		childArgs = append(childArgs, "--stream-baseurl="+streamBaseURL)
	}
	if nosignatures {
		childArgs = append(childArgs, "--no-signatures")
	}

	for _, arg := range args {
		releaseTmpf, err := ioutil.TempFile("", "release")
		if err != nil {
			return err
		}
		var archStreamName = streamName
		if !strings.HasPrefix(arg, "https://") {
			if distro != "rhcos" {
				return errors.New("Arguments must be https:// URLs (or with --distro rhcos, ARCH=VERSION)")
			}
			parts := strings.SplitN(arg, "=", 2)
			if len(parts) < 2 {
				return fmt.Errorf("Expecting ARCH=VERSION, found: %s", arg)
			}
			arch := parts[0]
			ver := parts[1]
			// Convert e.g. 48.82.<timestamp> to rhcos-4.8
			verSplit := strings.Split(ver, ".")
			archStreamName = fmt.Sprintf("rhcos-%s.%s", verSplit[0][0:1], verSplit[0][1:])
			if arch != "x86_64" {
				archStreamName += "-" + arch
			}
			endpoint := rhcosCosaEndpoint
			if streamBaseURL != "" {
				endpoint = streamBaseURL
			}
			base := fmt.Sprintf("%s/%s", endpoint, archStreamName)
			u := fmt.Sprintf("%s/%s/%s/meta.json", base, ver, arch)
			arg = u
			childArgs = append(childArgs, "--stream-baseurl="+endpoint)
		}
		cosaArgs := append([]string{}, childArgs...)
		cosaArgs = append(cosaArgs, "--url="+arg)
		cosaArgs = append(cosaArgs, "--stream-name="+archStreamName)
		cosaArgs = append(cosaArgs, "--output="+releaseTmpf.Name())
		c := exec.Command("cosa", cosaArgs...)
		c.Stderr = os.Stderr
		if err := c.Run(); err != nil {
			return err
		}

		var rel release.Release
		buf, err := ioutil.ReadAll(releaseTmpf)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(buf, &rel); err != nil {
			return err
		}
		relarches := rel.ToStreamArchitectures()
		for arch, relarchdata := range relarches {
			if _, ok := streamArches[arch]; ok {
				if target == "" {
					return fmt.Errorf("Duplicate architecture %s", arch)
				}
			}
			streamArches[arch] = relarchdata
		}
	}

	// Serialize to JSON
	var targetWriter io.Writer
	if target != "" {
		var err error
		targetWriter, err = os.Create(target)
		if err != nil {
			return err
		}
	} else {
		targetWriter = os.Stdout
	}
	encoder := json.NewEncoder(targetWriter)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(&outStream); err != nil {
		return fmt.Errorf("Error while encoding: %v", err)
	}
	return nil
}
