// Copyright 2018 CoreOS, Inc.
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
	"strings"

	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/platform"
)

// GenPodmanScratchContainer creates a podman scratch container out of binaries from the host
func GenPodmanScratchContainer(c cluster.TestCluster, m platform.Machine, name string, binnames []string) {
	// Scratch containers are created by copying a binary and its shared libraries dependencies
	// into the container image. `ldd` is used to find the paths to the shared libraries. On
	// power9, some shared libraries were symlinked to versioned shared libraries using the
	// versioned filename as the target. For example, libm.so.6 would be copied into the scratch
	// container as libm-2.28.so. When the versioned shared libraries were copied into the scratch
	// container, the dynamic linker could not find the non-versioned filenames. The ld.so.cache
	// seemed to have symlinks to the versioned shared libraries. Deleting /etc/ld.so.cache
	// restored symlinks to the non-versioned shared libraries.
	cmd := `tmpdir=$(mktemp -d); cd $tmpdir; echo -e "FROM scratch\nCOPY . /" > Dockerfile;
		sudo rm -f /etc/ld.so.cache;
		b=$(which %s); libs=$(sudo ldd $b | grep -o /lib'[^ ]*' | sort -u);
		sudo rsync -av --relative --copy-links $b $libs ./;
		sudo podman build --network host --layers=false -t localhost/%s .`
	c.RunCmdSyncf(m, cmd, strings.Join(binnames, " "), name)
}
