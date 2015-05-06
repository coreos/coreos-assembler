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

package kola

import (
	"flag"

	"github.com/coreos/mantle/platform"
)

var (
	qemuImage = flag.String("qemu.image",
		"/mnt/host/source/src/build/images/amd64-usr/latest/coreos_production_image.bin",
		"Base disk image for QEMU based tests.")

	gceImage       = flag.String("gce.image", "latest", "GCE image")
	gceProject     = flag.String("gce.project", "coreos-gce-testing", "GCE project")
	gceZone        = flag.String("gce.zone", "us-central1-a", "GCE zone")
	gceMachineType = flag.String("gce.machine", "n1-standard-1", "GCE machine type")
	gceDisk        = flag.String("gce.disk", "pd-ssd", "GCE disk type")
	gceBaseName    = flag.String("gce.basename", "kola", "GCE instance names will be generated from this")
	gceNetwork     = flag.String("gce.network", "default", "GCE network")
)

func gceOpts() *platform.GCEOpts {
	return &platform.GCEOpts{
		Image:       *gceImage,
		Project:     *gceProject,
		Zone:        *gceZone,
		MachineType: *gceMachineType,
		DiskType:    *gceDisk,
		BaseName:    *gceBaseName,
		Network:     *gceNetwork,
	}
}
