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

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/platform"
)

// GenPodmanScratchContainer creates a podman scratch container out of binaries from the host
func GenPodmanScratchContainer(c cluster.TestCluster, m platform.Machine, name string, binnames []string) {
	cmd := `tmpdir=$(mktemp -d); cd $tmpdir; echo -e "FROM scratch\nCOPY . /" > Dockerfile;
	        b=$(which %s); libs=$(sudo ldd $b | grep -o /lib'[^ ]*' | sort -u);
			sudo rsync -av --relative --copy-links $b $libs ./;
			sudo podman build --network host --layers=false -t localhost/%s .`
	c.RunCmdSyncf(m, cmd, strings.Join(binnames, " "), name)
}
