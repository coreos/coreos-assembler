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

package fcos

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
)

const canonicalStreamIndexLocation = "https://builds.coreos.fedoraproject.org/prod/streams/%s/releases.json"
const canonicalStreamMetadataLocation = "https://builds.coreos.fedoraproject.org/streams/%s.json"

// XXX: dedupe with fedora-coreos-stream-generator

// This models the release index:
// https://github.com/coreos/fedora-coreos-tracker/tree/master/metadata/release-index
type ReleaseIndex struct {
	Note     string                `json:"note"` // used to note to users not to consume the release metadata index
	Releases []ReleaseIndexRelease `json:"releases"`
	Metadata ReleaseIndexMetadata  `json:"metadata"`
	Stream   string                `json:"stream"`
}

type ReleaseIndexRelease struct {
	CommitHash []ReleaseCommit `json:"commits"`
	Version    string          `json:"version"`
	Endpoint   string          `json:"metadata"`
}

type ReleaseIndexMetadata struct {
	LastModified string `json:"last-modified"`
}

// This models release metadata:
// https://github.com/coreos/fedora-coreos-tracker/tree/master/metadata/release
type Release struct {
	Architectures map[string]ReleaseArchitecture `json:"architectures"`
}

type ReleaseArchitecture struct {
	Commit string                  `json:"commit"`
	Media  map[string]ReleaseMedia `json:"media"`
}

type ReleaseMedia struct {
	Images map[string]ReleaseAMI `json:"images"`
}

type ReleaseAMI struct {
	Image string `json:"image"`
}

type ReleaseCommit struct {
	Architecture string `json:"architecture"`
	Checksum     string `json:"checksum"`
}

// This models stream metadata:
// https://github.com/coreos/fedora-coreos-tracker/tree/master/metadata/stream
type StreamMetadata struct {
	Stream        string                 `json:"stream"`
	Metadata      Metadata               `json:"metadata"`
	Architectures map[string]*StreamArch `json:"architectures"`
}

// StreamArch release details for x86_64 architetcure
type StreamArch struct {
	Artifacts StreamArtifacts `json:"artifacts"`
	Images    *StreamImages   `json:"images,omitempty"`
}

// StreamArtifacts contains shipped artifacts list
type StreamArtifacts struct {
	Aliyun       *StreamMediaDetails `json:"aliyun,omitempty"`
	Aws          *StreamMediaDetails `json:"aws,omitempty"`
	Azure        *StreamMediaDetails `json:"azure,omitempty"`
	Digitalocean *StreamMediaDetails `json:"digitalocean,omitempty"`
	Exoscale     *StreamMediaDetails `json:"exoscale,omitempty"`
	Gcp          *StreamMediaDetails `json:"gcp,omitempty"`
	Metal        *StreamMediaDetails `json:"metal,omitempty"`
	Openstack    *StreamMediaDetails `json:"openstack,omitempty"`
	Packet       *StreamMediaDetails `json:"packet,omitempty"`
	Qemu         *StreamMediaDetails `json:"qemu,omitempty"`
	Virtualbox   *StreamMediaDetails `json:"virtualbox,omitempty"`
	Vmware       *StreamMediaDetails `json:"vmware,omitempty"`
}

// StreamMediaDetails contains image artifact and release detail
type StreamMediaDetails struct {
	Release string                  `json:"release"`
	Formats map[string]*ImageFormat `json:"formats"`
}

// StreamImages contains images available in cloud providers
type StreamImages struct {
	Aws          *StreamAwsImage   `json:"aws,omitempty"`
	Azure        *StreamCloudImage `json:"azure,omitempty"`
	Gcp          *StreamCloudImage `json:"gcp,omitempty"`
	Digitalocean *StreamCloudImage `json:"digitalocean,omitempty"`
	Packet       *StreamCloudImage `json:"packet,omitempty"`
}

// StreamCloudImage image for Cloud provider
type StreamCloudImage struct {
	Image string `json:"image,omitempty"`
}

// StreamAwsImage Aws images
type StreamAwsImage struct {
	Regions map[string]*StreamAwsAMI `json:"regions,omitempty"`
}

// StreamAwsAMI aws AMI detail
type StreamAwsAMI struct {
	Release string `json:"release"`
	Image   string `json:"image"`
}

// Metadata for stream
type Metadata struct {
	LastModified string `json:"last-modified"`
}

// ImageFormat contains Disk image details
type ImageFormat struct {
	Disk      *ImageType `json:"disk,omitempty"`
	Kernel    *ImageType `json:"kernel,omitempty"`
	Initramfs *ImageType `json:"initramfs,omitempty"`
}

// ImageType contains image detail
type ImageType struct {
	Location  string `json:"location"`
	Signature string `json:"signature"`
	Sha256    string `json:"sha256"`
}

func fetchURL(url string) ([]byte, error) {
	res, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	body, err := ioutil.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		return nil, err
	}

	return body, nil
}

func FetchAndParseReleaseIndex(url string) (*ReleaseIndex, error) {
	body, err := fetchURL(url)
	if err != nil {
		return nil, err
	}

	var index *ReleaseIndex
	if err = json.Unmarshal(body, &index); err != nil {
		return nil, err
	}

	return index, nil
}

func FetchAndParseCanonicalReleaseIndex(stream string) (*ReleaseIndex, error) {
	return FetchAndParseReleaseIndex(fmt.Sprintf(canonicalStreamIndexLocation, stream))
}

func FetchAndParseStreamMetadata(url string) (*StreamMetadata, error) {
	body, err := fetchURL(url)
	if err != nil {
		return nil, err
	}

	var meta *StreamMetadata
	if err = json.Unmarshal(body, &meta); err != nil {
		return nil, err
	}

	return meta, nil
}

func FetchAndParseCanonicalStreamMetadata(stream string) (*StreamMetadata, error) {
	return FetchAndParseStreamMetadata(fmt.Sprintf(canonicalStreamMetadataLocation, stream))
}
