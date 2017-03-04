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

package aws

import (
	"fmt"

	"github.com/coreos/mantle/cli"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/api/aws"
	"github.com/coreos/pkg/capnslog"
	"github.com/spf13/cobra"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "ore/aws")

	AWS = &cobra.Command{
		Use:   "aws [command]",
		Short: "aws image and vm utilities",
	}

	API             *aws.API
	region          string
	profileName     string
	accessKeyID     string
	secretAccessKey string
)

func init() {
	AWS.PersistentFlags().StringVar(&profileName, "profile", "", "aws profile name")
	AWS.PersistentFlags().StringVar(&accessKeyID, "access-id", "", "aws access key")
	AWS.PersistentFlags().StringVar(&secretAccessKey, "secret-key", "", "aws secret key")
	AWS.PersistentFlags().StringVar(&region, "region", "us-east-1", "aws region")
	cli.WrapPreRun(AWS, preflightCheck)
}

func preflightCheck(cmd *cobra.Command, args []string) error {
	plog.Debugf("Running AWS Preflight check. Region: %v", region)
	api, err := aws.New(&aws.Options{
		Region:  region,
		Profile: profileName,
		Options: &platform.Options{},
	})
	if err != nil {
		return fmt.Errorf("could not create AWS client: %v", err)
	}
	if err := api.PreflightCheck(); err != nil {
		return fmt.Errorf("could not complete AWS preflight check: %v", err)
	}

	plog.Debugf("Preflight check success; we have liftoff")
	API = api
	return nil
}
