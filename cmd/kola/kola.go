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
	"flag"

	"github.com/coreos/mantle/cli"
)

const (
	cliName        = "kola"
	cliDescription = "The CoreOS Superdeep Borehole"
	// http://en.wikipedia.org/wiki/Kola_Superdeep_Borehole
)

var kolaPlatform = flag.String("platform", "qemu", "compute platform to run kola tests on")

func main() {
	cli.Run(cliName, cliDescription)
}
