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

package system

import (
	"syscall"
	"testing"
)

func TestSplitFlags(t *testing.T) {
	data := []struct {
		opts  string
		flags uintptr
		extra string
	}{
		{"", 0, ""},
		{"nodev,nosuid,mode=755", syscall.MS_NOSUID | syscall.MS_NODEV, "mode=755"},
		{"mode=755,other", 0, "mode=755,other"},
		{"mode=755,nodev,other", syscall.MS_NODEV, "mode=755,other"},
	}

	for _, d := range data {
		f, e := splitFlags(d.opts)
		if f != d.flags {
			t.Errorf("bad flags for %q, got 0x%x wanted 0x%x", d.opts, f, d.flags)
		}
		if e != d.extra {
			t.Errorf("bad extra for %q, got %q wanted %q", d.opts, e, d.extra)
		}
	}
}
