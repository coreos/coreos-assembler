// Copyright 2016 CoreOS, Inc.
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

package targen

import (
	"bufio"
	"bytes"
	"path/filepath"
	"strings"

	"github.com/coreos/mantle/system/exec"
)

// return a slice of strings that are the library dependencies of binary.
// returns a nil slice if the binary is static
func ldd(binary string) ([]string, error) {
	c := exec.Command("ldd", binary)

	// let's not get LD_PRELOAD invovled.
	c.Env = []string{}

	out, err := c.CombinedOutput()
	if err != nil {
		// static binaries have no libs (barring dlopened ones, which we can't find)
		if strings.Contains(string(out), "not a dynamic executable") {
			return nil, nil
		}

		return nil, err
	}

	buf := bytes.NewBuffer(out)
	sc := bufio.NewScanner(buf)
	sc.Split(bufio.ScanWords)

	var libs []string
	for sc.Scan() {
		w := sc.Text()
		if filepath.IsAbs(w) {
			libs = append(libs, w)
		}
	}

	return libs, nil
}
