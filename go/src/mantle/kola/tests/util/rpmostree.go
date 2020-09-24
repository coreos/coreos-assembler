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

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/platform"
)

// RpmOstreeDeployment represents some of the data of an rpm-ostree deployment
type RpmOstreeDeployment struct {
	Booted            bool     `json:"booted"`
	Checksum          string   `json:"checksum"`
	Origin            string   `json:"origin"`
	Osname            string   `json:"osname"`
	Packages          []string `json:"packages"`
	RequestedPackages []string `json:"requested-packages"`
	Timestamp         int64    `json:"timestamp"`
	Unlocked          string   `json:"unlocked"`
	Version           string   `json:"version"`

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
	rpmOstreeJSON, err := c.SSH(m, "rpm-ostree status --json")
	if err != nil {
		return target, fmt.Errorf("Could not get rpm-ostree status: %v", err)
	}

	err = json.Unmarshal(rpmOstreeJSON, &target)
	if err != nil {
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
