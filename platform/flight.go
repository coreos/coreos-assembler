// Copyright 2015 CoreOS, Inc.
// Copyright 2018 Red Hat
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

package platform

import (
	"sync"
)

type BaseFlight struct {
	clusterlock sync.Mutex
	clustermap  map[string]Cluster

	platform   Name
	ctPlatform string
	baseopts   *Options
}

func NewBaseFlight(opts *Options, platform Name, ctPlatform string) (*BaseFlight, error) {
	bf := &BaseFlight{
		clustermap: make(map[string]Cluster),
		platform:   platform,
		ctPlatform: ctPlatform,
		baseopts:   opts,
	}

	return bf, nil
}

func (bf *BaseFlight) Platform() Name {
	return bf.platform
}

func (bf *BaseFlight) Clusters() []Cluster {
	bf.clusterlock.Lock()
	defer bf.clusterlock.Unlock()
	clusters := make([]Cluster, 0, len(bf.clustermap))
	for _, m := range bf.clustermap {
		clusters = append(clusters, m)
	}
	return clusters
}

func (bf *BaseFlight) AddCluster(c Cluster) {
	bf.clusterlock.Lock()
	defer bf.clusterlock.Unlock()
	bf.clustermap[c.Name()] = c
}

func (bf *BaseFlight) DelCluster(c Cluster) {
	bf.clusterlock.Lock()
	defer bf.clusterlock.Unlock()
	delete(bf.clustermap, c.Name())
}

// Destroy destroys each Cluster in the Flight.
func (bf *BaseFlight) Destroy() {
	for _, c := range bf.Clusters() {
		c.Destroy()
	}
}
