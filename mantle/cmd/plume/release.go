// Copyright 2016 CoreOS, Inc.
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

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/url"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/coreos/mantle/platform/api/aws"
	"github.com/coreos/stream-metadata-go/release"
	"github.com/spf13/cobra"
)

var (
	awsCredentialsFile string
	selectedDistro     string

	specBucket  string
	specPolicy  string
	specProfile string
	specRegion  string
	specStream  string
	specVersion string

	cmdRelease = &cobra.Command{
		Use:   "release [options]",
		Short: "Publish a new CoreOS release.",
		Run:   runRelease,
		Long:  `Publish a new CoreOS release.`,
	}
)

func init() {
	cmdRelease.Flags().StringVar(&awsCredentialsFile, "aws-credentials", "", "AWS credentials file")
	cmdRelease.Flags().StringVar(&selectedDistro, "distro", "fcos", "system to release")
	cmdRelease.Flags().StringVar(&specBucket, "bucket", "fcos-builds", "S3 bucket")
	cmdRelease.Flags().StringVar(&specPolicy, "policy", "public-read", "Canned ACL policy")
	cmdRelease.Flags().StringVar(&specProfile, "profile", "default", "AWS profile")
	cmdRelease.Flags().StringVar(&specRegion, "region", "us-east-1", "S3 bucket region")
	cmdRelease.Flags().StringVarP(&specStream, "stream", "S", "testing", "target stream")
	cmdRelease.Flags().StringVarP(&specVersion, "version", "V", "", "release version")
	root.AddCommand(cmdRelease)
}

func runRelease(cmd *cobra.Command, args []string) {
	switch selectedDistro {
	case "fcos":
		runFcosRelease(cmd, args)
	default:
		plog.Fatalf("Unknown distro %q:", selectedDistro)
	}
}

func runFcosRelease(cmd *cobra.Command, args []string) {
	if len(args) > 0 {
		plog.Fatal("No args accepted")
	}
	if specVersion == "" {
		plog.Fatal("--version is required")
	}
	if specStream == "" {
		plog.Fatal("--stream is required")
	}
	if specBucket == "" {
		plog.Fatal("--bucket is required")
	}
	if specRegion == "" {
		plog.Fatal("--region is required")
	}

	doS3()
	modifyReleaseMetadataIndex()
}

func doS3() {
	api, err := aws.New(&aws.Options{
		CredentialsFile: awsCredentialsFile,
		Profile:         specProfile,
		Region:          specRegion,
	})
	if err != nil {
		plog.Fatalf("creating aws client: %v", err)
	}

	// Assumes the bucket layout defined inside of
	// https://github.com/coreos/fedora-coreos-tracker/issues/189
	err = api.UpdateBucketObjectsACL(specBucket, filepath.Join("prod", "streams", specStream, "builds", specVersion), specPolicy)
	if err != nil {
		plog.Fatalf("updating object ACLs: %v", err)
	}
}

func modifyReleaseMetadataIndex() {
	api, err := aws.New(&aws.Options{
		CredentialsFile: awsCredentialsFile,
		Profile:         specProfile,
		Region:          specRegion,
	})
	if err != nil {
		plog.Fatalf("creating aws client: %v", err)
	}

	// Note we use S3 directly here instead of
	// FetchAndParseCanonicalReleaseIndex(), since that one uses the
	// CloudFronted URL and we need to be sure we're operating on the latest
	// version.  Plus we need S3 creds anyway later on to push the modified
	// release index back.

	path := filepath.Join("prod", "streams", specStream, "releases.json")
	data, err := func() ([]byte, error) {
		f, err := api.DownloadFile(specBucket, path)
		if err != nil {
			if awsErr, ok := err.(awserr.Error); ok {
				if awsErr.Code() == "NoSuchKey" {
					return []byte("{}"), nil
				}
			}
			return []byte{}, fmt.Errorf("downloading release metadata index: %v", err)
		}
		defer f.Close()
		d, err := ioutil.ReadAll(f)
		if err != nil {
			return []byte{}, fmt.Errorf("reading release metadata index: %v", err)
		}
		return d, nil
	}()
	if err != nil {
		plog.Fatal(err)
	}

	var releaseIdx release.Index
	err = json.Unmarshal(data, &releaseIdx)
	if err != nil {
		plog.Fatalf("unmarshaling release metadata json: %v", err)
	}

	releasePath := filepath.Join("prod", "streams", specStream, "builds", specVersion, "release.json")
	url, err := url.Parse(fmt.Sprintf("https://builds.coreos.fedoraproject.org/%s", releasePath))
	if err != nil {
		plog.Fatalf("creating metadata url: %v", err)
	}

	releaseFile, err := api.DownloadFile(specBucket, releasePath)
	if err != nil {
		plog.Fatalf("downloading release metadata at %s: %v", releasePath, err)
	}
	defer releaseFile.Close()

	releaseData, err := ioutil.ReadAll(releaseFile)
	if err != nil {
		plog.Fatalf("reading release metadata: %v", err)
	}

	var rel release.Release
	err = json.Unmarshal(releaseData, &rel)
	if err != nil {
		plog.Fatalf("unmarshaling release metadata: %v", err)
	}

	var commits []release.IndexReleaseCommit
	for arch, vals := range rel.Architectures {
		commits = append(commits, release.IndexReleaseCommit{
			Architecture: arch,
			Checksum:     vals.Commit,
		})
	}

	newIdxRelease := release.IndexRelease{
		Commits:     commits,
		Version:     specVersion,
		MetadataURL: url.String(),
	}

	for i, rel := range releaseIdx.Releases {
		if compareStaticReleaseInfo(rel, newIdxRelease) {
			if i != (len(releaseIdx.Releases) - 1) {
				plog.Fatalf("build is already present and is not the latest release")
			}

			comp := compareCommits(rel.Commits, newIdxRelease.Commits)
			if comp == 0 {
				// the build is already the latest release, exit
				return
			} else if comp == -1 {
				// the build is present and contains a subset of the new release data,
				// pop the old entry and add the new version
				releaseIdx.Releases = releaseIdx.Releases[:len(releaseIdx.Releases)-1]
				break
			} else {
				// the commit hash of the new build is not a superset of the current release
				plog.Fatalf("build is present but commit hashes are not a superset of latest release")
			}
		}
	}

	for _, archs := range rel.Architectures {
		awsmedia := archs.Media.Aws
		if awsmedia == nil {
			continue
		}
		for region, ami := range awsmedia.Images {
			aws_api, err := aws.New(&aws.Options{
				CredentialsFile: awsCredentialsFile,
				Profile:         specProfile,
				Region:          region,
			})
			if err != nil {
				plog.Fatalf("creating AWS API for modifying launch permissions: %v", err)
			}

			err = aws_api.PublishImage(ami.Image)
			if err != nil {
				plog.Fatalf("couldn't publish image in %v: %v", region, err)
			}
		}
	}

	releaseIdx.Releases = append(releaseIdx.Releases, newIdxRelease)

	releaseIdx.Metadata.LastModified = time.Now().UTC().Format("2006-01-02T15:04:05Z")
	releaseIdx.Note = "For use only by Fedora CoreOS internal tooling.  All other applications should obtain release info from stream metadata endpoints."
	releaseIdx.Stream = specStream

	out, err := json.Marshal(releaseIdx)
	if err != nil {
		plog.Fatalf("marshalling release metadata json: %v", err)
	}

	// we don't want this to be cached for very long so that e.g. Cincinnati picks it up quickly
	var releases_max_age = 60 * 5
	err = api.UploadObjectExt(bytes.NewReader(out), specBucket, path, true, specPolicy, aws.ContentTypeJSON, releases_max_age)
	if err != nil {
		plog.Fatalf("uploading release metadata json: %v", err)
	}
}

func compareStaticReleaseInfo(a, b release.IndexRelease) bool {
	if a.Version != b.Version || a.MetadataURL != b.MetadataURL {
		return false
	}
	return true
}

// returns -1 if a is a subset of b, 0 if equal, 1 if a is not a subset of b
func compareCommits(a, b []release.IndexReleaseCommit) int {
	if len(a) > len(b) {
		return 1
	}
	sameLength := len(a) == len(b)
	for _, aHash := range a {
		found := false
		for _, bHash := range b {
			if aHash.Architecture == bHash.Architecture && aHash.Checksum == bHash.Checksum {
				found = true
				break
			}
		}
		if !found {
			return 1
		}
	}
	if sameLength {
		return 0
	}
	return -1
}
