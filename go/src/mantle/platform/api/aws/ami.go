// Copyright 2017 CoreOS, Inc.
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

package aws

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// relaseAMIs matches the structure of the AMIs listed in our
// coreos_production_ami_all.json release file
type releaseAMIs struct {
	AMIS []struct {
		Name string `json:"name"`
		HVM  string `json:"hvm"`
	} `json:"amis"`
}

var amiCache struct {
	alphaOnce sync.Once
	alphaAMIs *releaseAMIs

	betaOnce sync.Once
	betaAMIs *releaseAMIs

	stableOnce sync.Once
	stableAMIs *releaseAMIs
}

// resolveAMI is used to minimize network requests while allowing resolution of
// release channels to specific AMI ids.
// If any issue occurs attempting to resolve a given AMI, e.g. a network error,
// this method panics.
func resolveAMI(ami string, region string) string {
	resolveChannel := func(channel string) *releaseAMIs {
		resp, err := http.DefaultClient.Get(fmt.Sprintf("https://%s.release.core-os.net/amd64-usr/current/coreos_production_ami_all.json", channel))
		if err != nil {
			panic(fmt.Errorf("unable to fetch %v AMI json: %v", channel, err))
		}

		var amis releaseAMIs
		err = json.NewDecoder(resp.Body).Decode(&amis)
		if err != nil {
			panic(fmt.Errorf("unable to parse release bucket %v AMI: %v", channel, err))
		}
		return &amis
	}

	var channelAmis *releaseAMIs
	switch ami {
	case "alpha":
		amiCache.alphaOnce.Do(func() {
			amiCache.alphaAMIs = resolveChannel(ami)
		})
		channelAmis = amiCache.alphaAMIs
	case "beta":
		amiCache.betaOnce.Do(func() {
			amiCache.betaAMIs = resolveChannel(ami)
		})
		channelAmis = amiCache.betaAMIs
	case "stable":
		amiCache.stableOnce.Do(func() {
			amiCache.stableAMIs = resolveChannel(ami)
		})
		channelAmis = amiCache.stableAMIs
	default:
		return ami
	}

	for _, ami := range channelAmis.AMIS {
		if ami.Name == region {
			return ami.HVM
		}
	}
	panic(fmt.Sprintf("could not find %v ami in %+v", ami, amiCache.alphaAMIs.AMIS))
}
