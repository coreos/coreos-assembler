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
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/api/gcloud"
)

// GlobalOptions are set in main and represent options that affect all tests
// run in a single invocation of pluton.
type GlobalOptions struct {
	CloudPlatform   string // only GCE is supported currently
	PlatformOptions platform.Options
	GCEOptions      gcloud.Options

	Parallel  int
	OutputDir string

	BootkubeRepo      string
	BootkubeTag       string
	BootkubeScriptDir string
}

// Glue variable for setting global options
var Opts GlobalOptions
