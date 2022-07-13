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
	"net/url"
	"os"
	"path/filepath"

	"github.com/coreos/stream-metadata-go/stream"
	"github.com/spf13/cobra"
)

var (
	cmdStreamMirror = &cobra.Command{
		Use:   "stream-mirror [options]",
		Short: "Copy all content of a stream JSON to a local path, optionally rewriting the base URL",
		RunE:  runStreamMirror,
		Args:  cobra.ExactArgs(0),

		SilenceUsage: true,
	}

	newBaseURLArg string
	srcFile       string
	destFile      string
	dest          string

	artifactTypes []string

	newBaseURL *url.URL
)

func init() {
	cmdStreamMirror.Flags().StringVar(&srcFile, "src-file", "", "Source path for stream JSON")
	if err := cmdStreamMirror.MarkFlagRequired("src-file"); err != nil {
		panic(err)
	}
	cmdStreamMirror.Flags().StringVar(&dest, "dest", "", "Write images to this directory")
	if err := cmdStreamMirror.MarkFlagRequired("dest"); err != nil {
		panic(err)
	}
	cmdStreamMirror.Flags().StringVar(&destFile, "dest-file", "", "Destination path for stream JSON (only useful with --url)")
	cmdStreamMirror.Flags().StringVar(&newBaseURLArg, "url", "", "New base URL for build")
	cmdStreamMirror.Flags().StringArrayVarP(&artifactTypes, "artifact", "a", nil, "Only fetch this specific artifact type")

	root.AddCommand(cmdStreamMirror)
}

func downloadArtifact(a *stream.Artifact) error {
	name, err := a.Name()
	if err != nil {
		return err
	}
	destfile := filepath.Join(dest, name)
	if _, err := os.Stat(destfile); err == nil {
		fmt.Printf("Skipping extant: %s\n", destfile)
		return nil
	}

	fmt.Printf("Downloading: %s\n", a.Location)
	path, err := a.Download(dest)
	if err != nil {
		return err
	}
	// Shouldn't happen but let's double check
	if path != destfile {
		return fmt.Errorf("Unexpected downloaded path: %s vs %s", path, destfile)
	}
	fmt.Printf("Download complete: %s\n", destfile)

	return nil
}

func rewriteURL(u string) (string, error) {
	loc, err := url.Parse(u)
	if err != nil {
		return "", err
	}
	name := filepath.Base(loc.Path)

	newURL := *newBaseURL
	newURL.Path = filepath.Join(newURL.Path, name)
	return newURL.String(), nil
}

func rewriteArtifact(a *stream.Artifact) error {
	if newBaseURL == nil {
		return nil
	}
	loc, err := rewriteURL(a.Location)
	if err != nil {
		return err
	}
	a.Location = loc
	if a.Signature != "" {
		loc, err := rewriteURL(a.Signature)
		if err != nil {
			return err
		}
		a.Signature = loc
	}
	return nil
}

func runStreamMirror(cmd *cobra.Command, args []string) error {
	if newBaseURLArg != "" {
		var err error
		newBaseURL, err = url.Parse(newBaseURLArg)
		if err != nil {
			return err
		}
	}

	if newBaseURL != nil && destFile == "" {
		return fmt.Errorf("Must specify --dest-file with --url")
	}
	var srcStream stream.Stream
	buf, err := ioutil.ReadFile(srcFile)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(buf, &srcStream); err != nil {
		return fmt.Errorf("failed to parse stream: %w", err)
	}

	matchingArtifacts := len(artifactTypes) > 0
	onlyArtifactTypes := make(map[string]bool)
	for _, a := range artifactTypes {
		onlyArtifactTypes[a] = true
	}

	for archName, arch := range srcStream.Architectures {
		fmt.Printf("Mirroring architecture: %s\n", archName)
		for artifactName, artifact := range arch.Artifacts {
			matches := !matchingArtifacts || onlyArtifactTypes[artifactName]
			for _, format := range artifact.Formats {
				for _, a := range []*stream.Artifact{format.Disk, format.Kernel, format.Initramfs, format.Rootfs} {
					if a != nil {
						if matches {
							err := downloadArtifact(a)
							if err != nil {
								return fmt.Errorf("failed to download artifact: %w", err)
							}
						} else {
							fmt.Printf("(skipped %s)\n", a.Location)
						}
						if err := rewriteArtifact(a); err != nil {
							return err
						}
					}
				}
			}
		}
	}

	if destFile != "" {
		buf, err := json.Marshal(srcStream)
		if err != nil {
			return err
		}
		err = ioutil.WriteFile(destFile, buf, 0644)
		if err != nil {
			return err
		}
		fmt.Printf("Wrote: %s\n", destFile)
	}

	return nil
}
