// Copyright 2017 CoreOS, Inc.
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

package harness

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/coreos/mantle/harness"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/machine/gcloud"
	"github.com/coreos/mantle/pluton"
	"github.com/coreos/mantle/pluton/spawn"
)

// Call this from main after setting all the global options. Tests are filtered
// by name based on the glob pattern given.
func RunSuite(pattern string) {
	Opts.GCEOptions.Options = &Opts.PlatformOptions

	tests, err := filterTests(Tests, pattern)
	if err != nil {
		fmt.Printf("Error filtering glob pattern: %v", err)
		os.Exit(1)

	}

	opts := harness.Options{
		OutputDir: Opts.OutputDir,
		Parallel:  Opts.Parallel,
		Verbose:   true,
	}
	suite := harness.NewSuite(opts, tests)

	if err := suite.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Println("FAIL")
		os.Exit(1)
	}
	fmt.Println("PASS")
	os.Exit(0)
}

func filterTests(tests harness.Tests, pattern string) (harness.Tests, error) {
	var filteredTests = make(harness.Tests)
	for name, t := range tests {
		match, err := filepath.Match(pattern, name)
		if err != nil {
			return nil, err
		}
		if !match {
			continue
		}
		filteredTests[name] = t
	}
	return filteredTests, nil
}

// RunTest is called inside the closure passed into the harness. Currently only
// GCE is supported, no reason this can't change
func runTest(t pluton.Test, h *harness.H) {
	h.Parallel()

	var cloud platform.Cluster
	var err error

	switch Opts.CloudPlatform {
	case "gce":
		cloud, err = gcloud.NewCluster(&Opts.GCEOptions, h.OutputDir())
	default:
		err = fmt.Errorf("invalid cloud platform %v", Opts.CloudPlatform)
	}

	if err != nil {
		h.Fatalf("Cluster failed: %v", err)
	}
	defer func() {
		if err := cloud.Destroy(); err != nil {
			h.Logf("cluster.Destroy(): %v", err)
		}
	}()

	config := spawn.BootkubeConfig{
		ImageRepo:      Opts.BootkubeRepo,
		ImageTag:       Opts.BootkubeTag,
		ScriptDir:      Opts.BootkubeScriptDir,
		InitialWorkers: t.Options.InitialWorkers,
		InitialMasters: t.Options.InitialMasters,
		SelfHostEtcd:   t.Options.SelfHostEtcd,
	}

	c, err := spawn.MakeBootkubeCluster(cloud, config)
	if err != nil {
		h.Fatalf("creating cluster: %v", err)
	}

	// TODO(pb): evidence that harness and spawn should be the same package?
	c.H = h

	t.Run(c)
}
