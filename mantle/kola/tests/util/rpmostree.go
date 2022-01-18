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

package util

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/util"
	"github.com/coreos/pkg/capnslog"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "kola/tests/util/rpmostree")
)

// RpmOstreeDeployment represents some of the data of an rpm-ostree deployment
type RpmOstreeDeployment struct {
	Booted                 bool     `json:"booted"`
	Checksum               string   `json:"checksum"`
	Origin                 string   `json:"origin"`
	Osname                 string   `json:"osname"`
	Packages               []string `json:"packages"`
	RequestedPackages      []string `json:"requested-packages"`
	RequestedLocalPackages []string `json:"requested-local-packages"`
	Timestamp              int64    `json:"timestamp"`
	Unlocked               string   `json:"unlocked"`
	Version                string   `json:"version"`

	// instead of making it a generic map of strings to "value", we just
	// special-case the keys we're interested in for now
	BaseCommitMeta rpmOstreeBaseCommitMeta `json:"base-commit-meta"`
}

type rpmOstreeBaseCommitMeta struct {
	FedoraCoreOSStream string `json:"fedora-coreos.stream"`
}

// simplifiedRpmOstreeStatus contains deployments from rpm-ostree status
type simplifiedRpmOstreeStatus struct {
	Deployments []RpmOstreeDeployment
}

// GetRpmOstreeStatusJSON returns an unmarshal'ed JSON object that contains
// a limited representation of the output of `rpm-ostree status --json`
func GetRpmOstreeStatusJSON(c cluster.TestCluster, m platform.Machine) (simplifiedRpmOstreeStatus, error) {
	target := simplifiedRpmOstreeStatus{}
	// We have a case where the rpm-ostree status command is failing
	// for the ostree.hotfix test and we don't know why:
	// https://github.com/coreos/fedora-coreos-tracker/issues/942
	// Let's run it once and check the failure. If it fails we will
	// always return a failure because we want to know, but we will
	// also run it with some retries to see if it succeeds in a
	// successive try for some reason or if it continues to fail.
	rpmOstreeJSON, err := c.SSH(m, "rpm-ostree status --json")
	if err != nil {
		retryStatus := func() error {
			_, err := c.SSH(m, "rpm-ostree status --json")
			return err
		}
		err2 := util.Retry(10, 10*time.Second, retryStatus)
		if err2 != nil {
			plog.Errorf("rpm-ostree status failed even after retries: %v", err2)
		} else {
			plog.Warning("rpm-ostree status succeeded after retries.")
		}
		return target, err // the original error
	}

	if err := json.Unmarshal(rpmOstreeJSON, &target); err != nil {
		return target, fmt.Errorf("Couldn't umarshal the rpm-ostree status JSON data: %v", err)
	}

	return target, nil
}

func GetBootedDeployment(c cluster.TestCluster, m platform.Machine) (*RpmOstreeDeployment, error) {
	s, err := GetRpmOstreeStatusJSON(c, m)
	if err != nil {
		return nil, err
	}

	for _, d := range s.Deployments {
		if d.Booted {
			return &d, nil
		}
	}

	return nil, errors.New("No booted deployment found")
}
