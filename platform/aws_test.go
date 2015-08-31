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

package platform

import (
	"fmt"
	"os"
	"testing"
)

// These tests needs AWS_REGION, AWS_ACCESS_KEY_ID, and AWS_SECRET_ACCESS_KEY
// set to run.

func awsEnvCheck() error {
	envs := []string{"AWS_REGION", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY"}
	for _, e := range envs {
		if os.Getenv(e) == "" {
			return fmt.Errorf("missing aws environment variable %q", e)
		}
	}

	return nil
}

func TestAWSMachine(t *testing.T) {
	if err := awsEnvCheck(); err != nil {
		t.Skip(err)
	}

	c, err := NewAWSCluster(AWSOptions{})
	if err != nil {
		t.Error(err)
		return
	}

	defer c.Destroy()

	m, err := c.NewMachine("")
	if err != nil {
		t.Error(err)
		return
	}

	defer m.Destroy()
}
