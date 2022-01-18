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
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/kola/tests/util"
	"github.com/coreos/mantle/platform"
)

// the "basic" test is only supported on 'rhcos' for now because of how
// the refs are defined. if 'fcos' goes in the same direction, we can
// expand support there.
func init() {
	register.RegisterTest(&register.Test{
		Run:         ostreeBasicTest,
		ClusterSize: 1,
		Name:        "ostree.basic",
		Distros:     []string{"rhcos"},
		FailFast:    true,
		Tags:        []string{"ostree"},
	})

	register.RegisterTest(&register.Test{
		Run:         ostreeRemoteTest,
		ClusterSize: 1,
		Name:        "ostree.remote",
		Flags:       []register.Flag{register.RequiresInternetAccess}, // need network to contact remote
		FailFast:    true,
		Tags:        []string{"ostree"},
	})
}

type ostreeAdminStatus struct {
	Checksum string
	Origin   string
	Version  string
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

// getOstreeAdminStatus stuffs the important output of `ostree admin status`
// into an `ostreeAdminStatus` struct
func getOstreeAdminStatus(c cluster.TestCluster, m platform.Machine) (ostreeAdminStatus, error) {
	oaStatus := ostreeAdminStatus{}

	oasOutput, err := c.SSH(m, "ostree admin status")
	if err != nil {
		return oaStatus, fmt.Errorf(`Could not get "ostree admin status": %v`, err)
	}

	oasSplit := strings.Split(string(oasOutput), "\n")
	if len(oasSplit) < 3 {
		return oaStatus, fmt.Errorf(`Unexpected output from "ostree admin status": %v`, string(oasOutput))
	}

	// we use a bunch of regexps to find the content in each line of output
	// from "ostree admin status".  the `match` for each line sticks the
	// captured group as the last element of the array; that is used as the
	// value for each field of the struct
	reCsum, _ := regexp.Compile(`^\* [\w\-]+ ([0-9a-f]+)\.\d`)
	csumMatch := reCsum.FindStringSubmatch(oasSplit[0])
	if csumMatch == nil {
		return oaStatus, fmt.Errorf(`Could not parse first line from "ostree admin status": %q`, oasSplit[0])
	}
	oaStatus.Checksum = csumMatch[len(csumMatch)-1]

	reVersion, _ := regexp.Compile(`^Version: (.*)`)
	versionMatch := reVersion.FindStringSubmatch(strings.TrimSpace(oasSplit[1]))
	if versionMatch == nil {
		return oaStatus, fmt.Errorf(`Could not parse second line from "ostree admin status": %q`, oasSplit[1])
	}
	oaStatus.Version = versionMatch[len(versionMatch)-1]

	reOrigin, _ := regexp.Compile(`^origin refspec: (.*)`)
	originMatch := reOrigin.FindStringSubmatch(strings.TrimSpace(oasSplit[2]))
	if originMatch == nil {
		return oaStatus, fmt.Errorf(`Could not parse third line from "ostree admin status": %q`, oasSplit[2])
	}
	oaStatus.Origin = originMatch[len(originMatch)-1]

	return oaStatus, nil
}

// ostreeBasicTest performs sanity checks on the output from `ostree admin status`,
// `ostree rev-parse`, and `ostree show` by comparing to the output from `rpm-ostree status`
func ostreeBasicTest(c cluster.TestCluster) {
	m := c.Machines()[0]

	ros, err := util.GetRpmOstreeStatusJSON(c, m)
	if err != nil {
		c.Fatal(err)
	}

	oas, err := getOstreeAdminStatus(c, m)
	if err != nil {
		c.Fatal(err)
	}

	if len(ros.Deployments) < 1 {
		c.Fatalf(`Did not find any deployments?!`)
	}

	// verify the output from `ostree admin status`
	c.RunLogged("admin status", func(c cluster.TestCluster) {
		if oas.Checksum != ros.Deployments[0].Checksum {
			c.Fatalf(`Checksums do not match; expected %q, got %q`, ros.Deployments[0].Checksum, oas.Checksum)
		}
		if oas.Version != ros.Deployments[0].Version {
			c.Fatalf(`Versions do not match; expected %q, got %q`, ros.Deployments[0].Version, oas.Version)
		}
		if oas.Origin != ros.Deployments[0].Origin {
			c.Fatalf(`Origins do not match; expected %q, got %q`, ros.Deployments[0].Origin, oas.Origin)
		}
	})

	// verify the output from `ostree rev-parse`
	// this is kind of moot since the origin for RHCOS is just
	// the checksum now
	c.RunLogged("rev-parse", func(c cluster.TestCluster) {
		// check the output of `ostree rev-parse`
		c.AssertCmdOutputContains(m, ("ostree rev-parse " + oas.Origin), oas.Checksum)
	})

	// verify the output of 'ostree show'
	c.RunLogged("show", func(c cluster.TestCluster) {
		oShowOut := c.MustSSH(m, ("ostree show " + oas.Checksum))
		oShowOutSplit := strings.Split(string(oShowOut), "\n")
		// we need at least the first 4 lines (commit, ContentChecksum, Date, Version)
		// to proceed safely
		if len(oShowOutSplit) < 4 {
			c.Fatalf(`Unexpected output from "ostree show": %q`, string(oShowOut))
		}

		// convert the 'timestamp' from `rpm-ostree status` into a date that
		// we can compare to the date in `ostree admin status`
		// also, wtf is up with formatting date/time in golang?!
		timeFormat := "2006-01-02 15:04:05 +0000"
		tsUnix := time.Unix(ros.Deployments[0].Timestamp, 0).UTC()
		tsFormatted := tsUnix.Format(timeFormat)
		oShowDate := strings.TrimPrefix(oShowOutSplit[2], "Date:  ")

		if oShowDate != tsFormatted {
			c.Fatalf(`Dates do not match; expected %q, got %q`, tsFormatted, oShowDate)
		}

		oVersionSplit := strings.Fields(oShowOutSplit[3])
		if len(oVersionSplit) < 2 {
			c.Fatalf(`Unexpected content in "Version" field of "ostree show" output: %q`, oShowOutSplit[3])
		}
		if oVersionSplit[1] != ros.Deployments[0].Version {
			c.Fatalf(`Versions do not match; expected %q, got %q`, ros.Deployments[0].Version, oVersionSplit[1])
		}
	})
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
	remoteUrl := "https://dl.fedoraproject.org/atomic/repo/"

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
