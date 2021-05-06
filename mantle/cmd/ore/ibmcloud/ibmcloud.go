// Copyright 2021 Red Hat
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

// IBMCloud uses api key for authentication to interact with the CLI/APIs: https://cloud.ibm.com/docs/account?topic=account-userapikey

package ibmcloud

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/user"
	"path/filepath"

	"github.com/coreos/mantle/cli"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/api/ibmcloud"
	"github.com/coreos/pkg/capnslog"
	"github.com/spf13/cobra"
)

type apiKeyFile struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	CreatedAt   string `json:"createdAt,omitempty"`
	ApiKey      string `json:"apikey"`
}

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "ore/ibmcloud")

	IbmCloud = &cobra.Command{
		Use:   "ibmcloud [command]",
		Short: "ibmcloud image utilities",
	}

	API             *ibmcloud.API
	region          string
	credentialsFile string
	apiKey          string
)

func init() {
	defaultRegion := os.Getenv("IBMCLOUD_REGION")
	if defaultRegion == "" {
		defaultRegion = "us-east"
	}

	IbmCloud.PersistentFlags().StringVar(&credentialsFile, "credentials-file", "", "IBMCloud credentials file")
	IbmCloud.PersistentFlags().StringVar(&apiKey, "api-key", "", "IBMCloud access key")
	IbmCloud.PersistentFlags().StringVar(&region, "region", defaultRegion, "IBMCloud region")
	cli.WrapPreRun(IbmCloud, preflightCheck)
}

func preflightCheck(cmd *cobra.Command, args []string) error {
	plog.Debugf("Running IBMCloud Preflight check. Region: %v", region)

	// if api key is not specified search the credentials file
	if apiKey == "" {
		// check if credentials file exists
		if credentialsFile != "" {
			credentialsFile, _ = filepath.Abs(credentialsFile)
		} else {
			plog.Debugf("credentials file not provided - checking default file")
			currentUser, err := user.Current()
			if err != nil {
				fmt.Fprintf(os.Stderr, "error getting current user info: %v\n", err)
				os.Exit(1)
			}
			homedir := currentUser.HomeDir
			credentialsFile = filepath.Join(homedir, ".bluemix/apikey.json")
		}

		file, err := os.Open(credentialsFile)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "credentials file does not exist: %v\n", err)
				os.Exit(1)
			} else {
				fmt.Fprintf(os.Stderr, "could not open credentials file: %v\n", err)
				os.Exit(1)
			}
		}
		defer file.Close()

		var apiKeyValues apiKeyFile
		bytes, err := ioutil.ReadAll(file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "could not read apikey file: %v\n", err)
			os.Exit(1)
		}
		err = json.Unmarshal(bytes, &apiKeyValues)
		if err != nil {
			fmt.Fprintf(os.Stderr, "could not parse api key json file: %v\n", err)
			os.Exit(1)
		}
		apiKey = apiKeyValues.ApiKey
	}

	api, err := ibmcloud.New(&ibmcloud.Options{
		ApiKey:          apiKey,
		CredentialsFile: credentialsFile,
		Options:         &platform.Options{},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not create IBMCloud client: %v\n", err)
		os.Exit(1)
	}
	plog.Debugf("Preflight check success; we have liftoff")
	API = api
	return nil
}
