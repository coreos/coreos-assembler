// Copyright 2020 Red Hat
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

package ignition

import (
	"fmt"
	"strconv"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
)

const ignitionJournalMsgId = "57124006b5c94805b77ce473e92a8aeb"

func init() {
	register.RegisterTest(&register.Test{
		Name:        "coreos.ignition.journald-log",
		Run:         sendJournaldLog,
		ClusterSize: 1,
		// Since RHCOS uses the 2x spec and not 3x.
		ExcludeDistros: []string{"rhcos"},
	})
}

func sendJournaldLog(c cluster.TestCluster) {
	m := c.Machines()[0]
	// See https://github.com/coreos/ignition/pull/958
	// for the MESSAGE_ID source. It will track the
	// journal messages related to an ignition config
	// provided by the user.
	out := c.MustSSH(m, fmt.Sprintf("journalctl -o json-pretty MESSAGE_ID=%s | jq -s '.[] | select(.IGNITION_CONFIG_TYPE == \"user\")' | wc -l", ignitionJournalMsgId))
	num, _ := strconv.Atoi(string(out))
	if num == 0 {
		c.Fatalf("Ignition didn't write %s", ignitionJournalMsgId)
	}
}
