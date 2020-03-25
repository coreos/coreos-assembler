// Copyright 2020 Red Hat, Inc.
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

package system

import (
	"io/ioutil"
	"os"
	"strconv"
	"strings"

	"github.com/pkg/errors"

	"github.com/coreos/mantle/system/exec"
)

// GetProcessors returns a count for number of cores we should use;
// this value is appropriate to pass to e.g. make -J as well as
// qemu -smp for example.
func GetProcessors() (uint, error) {
	// Note this code originated in cmdlib.sh; the git history there will
	// have a bit more info.
	proc1cgroup, err := ioutil.ReadFile("/proc/1/cgroup")
	if err != nil {
		if !os.IsNotExist(err) {
			return 0, err
		}
	} else {
		// only use 1 core on kubernetes since we can't determine how much we can actually use
		if strings.Contains(string(proc1cgroup), "kubepods") {
			return 1, nil
		}
	}

	nprocBuf, err := exec.Command("nproc").CombinedOutput()
	if err != nil {
		return 0, errors.Wrapf(err, "executing nproc")
	}

	nproc, err := strconv.ParseInt(strings.TrimSpace(string(nprocBuf)), 10, 32)
	if err != nil {
		return 0, errors.Wrapf(err, "parsing nproc output")
	}

	return uint(nproc), nil
}
