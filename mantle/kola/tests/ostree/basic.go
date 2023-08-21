// Copyright 2018 Red Hat, Inc.
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

package ostree

import (
	"regexp"
	"strings"

	"github.com/coreos/coreos-assembler/mantle/kola"
	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/kola/register"
	"github.com/coreos/coreos-assembler/mantle/platform"
)

func init() {
	register.RegisterTest(&register.Test{
		Run:         ostreeRemoteTest,
		ClusterSize: 1,
		Name:        "ostree.remote",
		Description: "Verify the ostree remote functions work.",
		FailFast:    true,
		Tags:        []string{"ostree", kola.NeedsInternetTag}, // need network to contact remote
	})
}

// getOstreeRemotes returns the current number of ostree remotes on a machine
func getOstreeRemotes(c cluster.TestCluster, m platform.Machine) (int, []string) {
	remoteListOut := string(c.MustSSH(m, "ostree remote list"))
	numRemotes := 0
	// If we get anything other than an empty string calculate the results
	// NOTE: This is needed as splitting "" ends up providing a count of 1
	//       when the count should be 0
	remoteListRaw := strings.Split(remoteListOut, "\n")
	if remoteListOut != "" {
		numRemotes = len(remoteListRaw)
	}
	return numRemotes, remoteListRaw
}

// ostreeRemoteTest verifies the `ostree remote` functionality;
// specifically:  `add`, `delete`, `list`, `refs`, `show-url`, `summary`
func ostreeRemoteTest(c cluster.TestCluster) {
	m := c.Machines()[0]

	// Get the initial amount of remotes configured
	initialRemotesNum, _ := getOstreeRemotes(c, m)

	// TODO: if this remote ever changes, update the `refMatch` regexp
	// in the `ostree remote summary` test below
	remoteName := "custom"
	remoteUrl := "https://kojipkgs.fedoraproject.org/ostree/repo/"

	// verify `ostree remote add` is successful
	c.Run("add", func(c cluster.TestCluster) {
		osRemoteAddCmd := "sudo ostree remote add --no-gpg-verify " + remoteName + " " + remoteUrl
		c.RunCmdSync(m, osRemoteAddCmd)
	})

	// verify `ostree remote list`
	c.Run("list", func(c cluster.TestCluster) {
		osRemoteListOut := c.MustSSH(m, "ostree remote list -u")

		osRemoteListSplit := strings.Split(string(osRemoteListOut), "\n")
		// should have original remote + newly added remote
		if len(osRemoteListSplit) != initialRemotesNum+1 {
			c.Fatalf(`Did not find expected amount of ostree remotes: %q. Got %d. Expected %d`, string(osRemoteListOut), len(osRemoteListSplit), initialRemotesNum+1)
		}

		var remoteFound bool = false
		for _, v := range osRemoteListSplit {
			lSplit := strings.Fields(v)
			if len(lSplit) != 2 {
				c.Fatalf(`Unexpected format of ostree remote entry: %q`, v)
			}
			if lSplit[0] == remoteName && lSplit[1] == remoteUrl {
				remoteFound = true
			}
		}
		if !remoteFound {
			c.Fatalf(`Added remote was not found: %v`, string(osRemoteListOut))
		}
	})

	// verify `ostree remote show-url`
	c.Run("show-url", func(c cluster.TestCluster) {
		c.AssertCmdOutputContains(m, ("ostree remote show-url " + remoteName), remoteUrl)
	})

	// verify `ostree remote refs`
	c.Run("refs", func(c cluster.TestCluster) {
		osRemoteRefsOut := c.MustSSH(m, ("ostree remote refs " + remoteName))
		if len(strings.Split(string(osRemoteRefsOut), "\n")) < 1 {
			c.Fatalf(`Did not receive expected amount of refs from remote: %v`, string(osRemoteRefsOut))
		}
	})

	// verify `ostree remote summary`
	c.Run("summary", func(c cluster.TestCluster) {
		remoteRefsOut := c.MustSSH(m, ("ostree remote refs " + remoteName))
		remoteRefsOutSplit := strings.Split(string(remoteRefsOut), "\n")
		remoteRefsCount := len(remoteRefsOutSplit)
		if remoteRefsCount < 1 {
			c.Fatalf(`Did not find any refs on ostree remote: %q`, string(remoteRefsOut))
		}

		osRemoteSummaryOut := c.MustSSH(m, ("ostree remote summary " + remoteName))
		if len(strings.Split(string(osRemoteSummaryOut), "\n")) < 1 {
			c.Fatalf(`Did not receive expected summary content from remote: %v`, string(osRemoteSummaryOut))
		}

		// the remote summary *should* include the same amount of
		// refs as found in `ostree remote refs`
		var refCount int = 0

		// this matches the line from the output that includes the name of the ref.
		// the original remote used in this test is a Fedora remote, so we are checking
		// for refs particular to that remote.
		// TODO: if the remote ever changes, we'll have to change this regexp too.
		refMatch := regexp.MustCompile(`^\* fedora.*`)
		remoteSummaryOutSplit := strings.Split(string(osRemoteSummaryOut), "\n")
		// the remote should have at least one ref in the summary
		if len(remoteSummaryOutSplit) < 4 {
			c.Fatalf(`Did not find any refs in the remote summary: %q`, string(osRemoteSummaryOut))
		}
		for _, v := range remoteSummaryOutSplit {
			if refMatch.MatchString(v) {
				refCount += 1
			}
		}

		if refCount == 0 {
			c.Fatalf(`Did not find any refs in the remote summary that matched!`)
		}

		if refCount != remoteRefsCount {
			c.Fatalf(`Did not find correct number of refs in remote summary; expected %q, got %q`, remoteRefsCount, refCount)
		}
	})

	// verify `ostree remote delete`
	c.Run("delete", func(c cluster.TestCluster) {
		preRemotesOut := c.MustSSH(m, "ostree remote list")
		preNumRemotes := len(strings.Split(string(preRemotesOut), "\n"))

		if preNumRemotes < 1 {
			c.Fatalf(`No remotes configured on host: %q`, string(preRemotesOut))
		}

		c.RunCmdSync(m, ("sudo ostree remote delete " + remoteName))

		delNumRemotes, delRemoteListOut := getOstreeRemotes(c, m)
		if delNumRemotes >= preNumRemotes {
			c.Fatalf(`Number of remotes did not decrease after "ostree delete": %s`, delRemoteListOut)
		}
	})
}
