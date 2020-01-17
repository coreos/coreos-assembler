// Copyright 2020 Red Hat, Inc.
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

package cosa

// Build is the coreos-assembler `meta.json` which defines a build.
// This code was copied from openshift-installer's
// https://github.com/openshift/installer/blob/a0350404997b0493d7bb16aa2875e5c42879b069/pkg/rhcos/builds.go
// For now, copy-paste updates there.  Later, maybe consider making this a public API?
type Build struct {
	Ref           string `json:"ref"`
	OSTreeVersion string `json:"ostree-version"`
	AMIs          []struct {
		Region string `json:"name"`
		HVM    string `json:"hvm"`
	} `json:"amis"`
	Azure struct {
		Image string `json:"image"`
		URL   string `json:"url"`
	}
	GCP struct {
		Image string `json:"image"`
		URL   string `json:"url"`
	}
	BaseURI string `json:"baseURI"`
	Images  struct {
		QEMU struct {
			Path               string `json:"path"`
			SHA256             string `json:"sha256"`
			UncompressedSHA256 string `json:"uncompressed-sha256"`
		} `json:"qemu"`
		OpenStack struct {
			Path               string `json:"path"`
			SHA256             string `json:"sha256"`
			UncompressedSHA256 string `json:"uncompressed-sha256"`
		} `json:"openstack"`
	} `json:"images"`
	FedoraCoreOSParentVersion string `json:"fedora-coreos.parent-version"`
}
