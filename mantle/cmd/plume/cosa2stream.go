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
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"time"

	"github.com/coreos/stream-metadata-go/release"
	"github.com/coreos/stream-metadata-go/stream"
	"github.com/spf13/cobra"
)

var (
	cmdCosaBuildToStream = &cobra.Command{
		Use:   "cosa2stream [options]",
		Short: "Generate stream JSON from a coreos-assembler build",
		RunE:  runCosaBuildToStream,

		SilenceUsage: true,
	}

	streamBaseURL string
	streamName    string
)

func init() {
	cmdCosaBuildToStream.Flags().StringVar(&streamBaseURL, "url", "", "Base URL for build")
	cmdCosaBuildToStream.Flags().StringVar(&streamName, "name", "", "Stream name")
	cmdCosaBuildToStream.MarkFlagRequired("name")
	root.AddCommand(cmdCosaBuildToStream)
}

func runCosaBuildToStream(cmd *cobra.Command, args []string) error {
	childArgs := []string{"generate-release-meta", "--stream-name=" + streamName}
	if streamBaseURL != "" {
		childArgs = append(childArgs, "--stream-baseurl="+streamBaseURL)
	}

	streamArches := make(map[string]stream.Arch)
	for _, arg := range args {
		releaseTmpf, err := ioutil.TempFile("", "release")
		if err != nil {
			return err
		}
		cosaArgs := append([]string{}, childArgs...)
		cosaArgs = append(cosaArgs, []string{fmt.Sprintf("--url=" + arg), "--output=" + releaseTmpf.Name()}...)
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
				return fmt.Errorf("Duplicate architecture %s", arch)
			}
			streamArches[arch] = relarchdata
		}
	}

	// Generate output stream from release
	outStream := stream.Stream{
		Stream:        streamName,
		Metadata:      stream.Metadata{LastModified: time.Now().UTC().Format(time.RFC3339)},
		Architectures: streamArches,
	}

	// Serialize to JSON
	encoder := json.NewEncoder(os.Stdout)
	if err := encoder.Encode(&outStream); err != nil {
		return fmt.Errorf("Error while encoding: %v", err)
	}
	return nil
}
