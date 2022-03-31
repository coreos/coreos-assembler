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

package util

import (
	"bufio"
	"io"

	"github.com/coreos/pkg/capnslog"
)

var plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "util")

// LogFrom reads lines from reader r and sends them to logger l.
func LogFrom(l capnslog.LogLevel, r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		plog.Log(l, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		plog.Errorf("Reading %s failed: %v", r, err)
	}
}
