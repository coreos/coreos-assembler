// Copyright 2019 Red Hat Inc.
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
	"github.com/spf13/pflag"
)

var (
	specPolicy   string
	specCommitId string

	fcosSpec = fcosChannelSpec{
		Bucket:  "fcos-builds",
		Profile: "default",
		Region:  "us-east-1",
	}
)

func AddFcosSpecFlags(flags *pflag.FlagSet) {
	flags.StringVar(&specPolicy, "policy", "public-read", "Canned ACL policy")
	flags.StringVar(&specCommitId, "commit-id", "", "OSTree Commit ID")
}

func FcosValidateArguments() {
	if specVersion == "" {
		plog.Fatal("--version is required")
	}
	if specChannel == "" {
		plog.Fatal("--channel is required")
	}
	if specCommitId == "" {
		plog.Fatal("--commit-id is required")
	}
}

func FcosChannelSpec() fcosChannelSpec {
	return fcosSpec
}
