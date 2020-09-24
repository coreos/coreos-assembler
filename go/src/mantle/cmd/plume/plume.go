// Copyright 2014 CoreOS, Inc.
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
	"io/ioutil"
	"net/http"

	"github.com/coreos/pkg/capnslog"
	"github.com/spf13/cobra"

	"github.com/coreos/mantle/auth"
	"github.com/coreos/mantle/cli"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "plume")
	root = &cobra.Command{
		Use:   "plume [command]",
		Short: "The CoreOS release utility",
	}

	gceJSONKeyFile string
)

func init() {
	root.PersistentFlags().StringVar(&gceJSONKeyFile, "gce-json-key", "", "use a JSON key for authentication")
}

func getGoogleClient() (*http.Client, error) {
	if gceJSONKeyFile != "" {
		if b, err := ioutil.ReadFile(gceJSONKeyFile); err == nil {
			return auth.GoogleClientFromJSONKey(b)
		} else {
			return nil, err
		}
	}
	return auth.GoogleClient()
}

func main() {
	cli.Execute(root)
}
