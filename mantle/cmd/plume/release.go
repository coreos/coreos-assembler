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
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/spf13/cobra"
	"golang.org/x/net/context"
	"google.golang.org/api/compute/v0.alpha"
	gs "google.golang.org/api/storage/v1"

	"github.com/coreos/mantle/fcos"
	"github.com/coreos/mantle/platform/api/aws"
	"github.com/coreos/mantle/platform/api/azure"
	"github.com/coreos/mantle/platform/api/gcloud"
	"github.com/coreos/mantle/storage"
)

var (
	releaseDryRun bool
	cmdRelease    = &cobra.Command{
		Use:   "release [options]",
		Short: "Publish a new CoreOS release.",
		Run:   runRelease,
		Long:  `Publish a new CoreOS release.`,
	}
)

func init() {
	cmdRelease.Flags().StringVar(&awsCredentialsFile, "aws-credentials", "", "AWS credentials file")
	cmdRelease.Flags().StringVar(&selectedDistro, "distro", "fcos", "system to release")
	cmdRelease.Flags().StringVar(&azureProfile, "azure-profile", "", "Azure Profile json file")
	cmdRelease.Flags().BoolVarP(&releaseDryRun, "dry-run", "n", false,
		"perform a trial run, do not make changes")
	AddSpecFlags(cmdRelease.Flags())
	AddFedoraSpecFlags(cmdRelease.Flags())
	AddFcosSpecFlags(cmdRelease.Flags())
	root.AddCommand(cmdRelease)
}

func runRelease(cmd *cobra.Command, args []string) {
	switch selectedDistro {
	case "fcos":
		if err := runFcosRelease(cmd, args); err != nil {
			plog.Fatal(err)
		}
	case "fedora":
		if err := runFedoraRelease(cmd, args); err != nil {
			plog.Fatal(err)
		}
	default:
		plog.Fatalf("Unknown distro %q:", selectedDistro)
	}
}

func runFcosRelease(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		plog.Fatal("No args accepted")
	}

	spec := FcosChannelSpec()
	FcosValidateArguments()

	doS3(&spec)

	modifyReleaseMetadataIndex(&spec, specCommitId)

	return nil
}

func runFedoraRelease(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		plog.Fatal("No args accepted")
	}

	spec, err := ChannelFedoraSpec()
	if err != nil {
		return err
	}
	ctx := context.Background()
	client := &http.Client{}

	// Make AWS images public.
	doAWS(ctx, client, nil, &spec)

	return nil
}

func sanitizeVersion() string {
	v := strings.Replace(specVersion, ".", "-", -1)
	return strings.Replace(v, "+", "-", -1)
}

func gceWaitForImage(pending *gcloud.Pending) {
	plog.Infof("Waiting for image creation to finish...")
	pending.Interval = 3 * time.Second
	pending.Progress = func(_ string, _ time.Duration, op *compute.Operation) error {
		status := strings.ToLower(op.Status)
		if op.Progress != 0 {
			plog.Infof("Image creation is %s: %s % 2d%%", status, op.StatusMessage, op.Progress)
		} else {
			plog.Infof("Image creation is %s. %s", status, op.StatusMessage)
		}
		return nil
	}
	if err := pending.Wait(); err != nil {
		plog.Fatal(err)
	}
	plog.Info("Success!")
}

func gceUploadImage(spec *channelSpec, api *gcloud.API, obj *gs.Object, name, desc string) string {
	plog.Noticef("Creating GCE image %s", name)
	op, pending, err := api.CreateImage(&gcloud.ImageSpec{
		SourceImage: obj.MediaLink,
		Family:      spec.GCE.Family,
		Name:        name,
		Description: desc,
		Licenses:    spec.GCE.Licenses,
	}, false)
	if err != nil {
		plog.Fatalf("GCE image creation failed: %v", err)
	}

	gceWaitForImage(pending)

	return op.TargetLink
}

func doGCE(ctx context.Context, client *http.Client, src *storage.Bucket, spec *channelSpec) {
	if spec.GCE.Project == "" || spec.GCE.Image == "" {
		plog.Notice("GCE image creation disabled.")
		return
	}

	api, err := gcloud.New(&gcloud.Options{
		Project:     spec.GCE.Project,
		JSONKeyFile: gceJSONKeyFile,
	})
	if err != nil {
		plog.Fatalf("GCE client failed: %v", err)
	}

	nameVer := fmt.Sprintf("%s-%s-v", spec.GCE.Family, sanitizeVersion())
	date := time.Now().UTC()
	name := nameVer + date.Format("20060102")
	desc := fmt.Sprintf("%s, %s, %s published on %s", spec.GCE.Description,
		specVersion, specBoard, date.Format("2006-01-02"))

	images, err := api.ListImages(ctx, spec.GCE.Family+"-")
	if err != nil {
		plog.Fatal(err)
	}

	var conflicting, oldImages []*compute.Image
	for _, image := range images {
		if strings.HasPrefix(image.Name, nameVer) {
			conflicting = append(conflicting, image)
		} else {
			oldImages = append(oldImages, image)
		}
	}
	sort.Slice(oldImages, func(i, j int) bool {
		getCreation := func(image *compute.Image) time.Time {
			stamp, err := time.Parse(time.RFC3339, image.CreationTimestamp)
			if err != nil {
				plog.Fatalf("Couldn't parse timestamp %q: %v", image.CreationTimestamp, err)
			}
			return stamp
		}
		return getCreation(oldImages[i]).After(getCreation(oldImages[j]))
	})

	// Check for any with the same version but possibly different dates.
	var imageLink string
	if len(conflicting) > 1 {
		plog.Fatalf("Duplicate GCE images found: %v", conflicting)
	} else if len(conflicting) == 1 {
		image := conflicting[0]
		name = image.Name
		imageLink = image.SelfLink

		if image.Status == "FAILED" {
			plog.Fatalf("Found existing GCE image %q in state %q", name, image.Status)
		}

		plog.Noticef("GCE image already exists: %s", name)

		if releaseDryRun {
			return
		}

		if image.Status == "PENDING" {
			pending, err := api.GetPendingForImage(image)
			if err != nil {
				plog.Fatalf("Couldn't wait for image creation: %v", err)
			}
			gceWaitForImage(pending)
		}
	} else {
		obj := src.Object(src.Prefix() + spec.GCE.Image)
		if obj == nil {
			plog.Fatalf("GCE image not found %s%s", src.URL(), spec.GCE.Image)
		}

		if releaseDryRun {
			plog.Noticef("Would create GCE image %s", name)
			return
		}

		imageLink = gceUploadImage(spec, api, obj, name, desc)
	}

	if spec.GCE.Publish != "" {
		obj := gs.Object{
			Name:        src.Prefix() + spec.GCE.Publish,
			ContentType: "text/plain",
		}
		media := strings.NewReader(
			fmt.Sprintf("projects/%s/global/images/%s\n",
				spec.GCE.Project, name))
		if err := src.Upload(ctx, &obj, media); err != nil {
			plog.Fatal(err)
		}
	} else {
		plog.Notice("GCE image name publishing disabled.")
	}

	var pendings []*gcloud.Pending
	for _, old := range oldImages {
		if old.Deprecated != nil && old.Deprecated.State != "" {
			continue
		}
		plog.Noticef("Deprecating old image %s", old.Name)
		pending, err := api.DeprecateImage(old.Name, gcloud.DeprecationStateDeprecated, imageLink)
		if err != nil {
			plog.Fatal(err)
		}
		pending.Interval = 1 * time.Second
		pending.Timeout = 0
		pendings = append(pendings, pending)
	}

	if spec.GCE.Limit > 0 && len(oldImages) > spec.GCE.Limit {
		plog.Noticef("Pruning %d GCE images.", len(oldImages)-spec.GCE.Limit)
		for _, old := range oldImages[spec.GCE.Limit:] {
			if old.Name == "coreos-alpha-1122-0-0-v20160727" {
				plog.Noticef("%v: not deleting: hardcoded solution to hardcoded problem", old.Name)
				continue
			}
			plog.Noticef("Deleting old image %s", old.Name)
			pending, err := api.DeleteImage(old.Name)
			if err != nil {
				plog.Fatal(err)
			}
			pending.Interval = 1 * time.Second
			pending.Timeout = 0
			pendings = append(pendings, pending)
		}
	}

	plog.Infof("Waiting on %d operations.", len(pendings))
	for _, pending := range pendings {
		err := pending.Wait()
		if err != nil {
			plog.Fatal(err)
		}
	}
}

func doAzure(ctx context.Context, client *http.Client, src *storage.Bucket, spec *channelSpec) {
	if spec.Azure.StorageAccount == "" {
		plog.Notice("Azure image creation disabled.")
		return
	}

	// channel name should be caps for azure image
	imageName := fmt.Sprintf("%s-%s-%s", spec.Azure.Offer, strings.Title(specChannel), specVersion)

	for _, environment := range spec.Azure.Environments {
		api, err := azure.New(&azure.Options{
			AzureProfile:      azureProfile,
			AzureSubscription: environment.SubscriptionName,
		})
		if err != nil {
			plog.Fatalf("failed to create Azure API: %v", err)
		}

		if releaseDryRun {
			// TODO(bgilbert): check that the image exists
			plog.Printf("Would share %q on %v", imageName, environment.SubscriptionName)
			continue
		} else {
			plog.Printf("Sharing %q on %v...", imageName, environment.SubscriptionName)
		}

		if err := api.ShareImage(imageName, "public"); err != nil {
			plog.Fatalf("failed to share image %q: %v", imageName, err)
		}
	}
}

func doAWS(ctx context.Context, client *http.Client, src *storage.Bucket, spec *channelSpec) {
	if spec.AWS.Image == "" {
		plog.Notice("AWS image creation disabled.")
		return
	}

	awsImageMetadata, err := getSpecAWSImageMetadata(spec)
	if err != nil {
		return
	}

	imageName := awsImageMetadata["imageName"]

	for _, part := range spec.AWS.Partitions {
		for _, region := range part.Regions {
			if releaseDryRun {
				plog.Printf("Checking for images in %v %v...", part.Name, region)
			} else {
				plog.Printf("Publishing images in %v %v...", part.Name, region)
			}

			api, err := aws.New(&aws.Options{
				CredentialsFile: awsCredentialsFile,
				Profile:         part.Profile,
				Region:          region,
			})
			if err != nil {
				plog.Fatalf("creating client for %v %v: %v", part.Name, region, err)
			}

			publish := func(imageName string) {
				imageID, err := api.FindImage(imageName)
				if err != nil {
					plog.Fatalf("couldn't find image %q in %v %v: %v", imageName, part.Name, region, err)
				}

				if !releaseDryRun {
					err := api.PublishImage(imageID)
					if err != nil {
						plog.Fatalf("couldn't publish image in %v %v: %v", part.Name, region, err)
					}
				}
			}
			publish(imageName + "-hvm")
		}
	}
}

func doS3(spec *fcosChannelSpec) {
	api, err := aws.New(&aws.Options{
		CredentialsFile: awsCredentialsFile,
		Profile:         spec.Profile,
		Region:          spec.Region,
	})
	if err != nil {
		plog.Fatalf("creating aws client: %v", err)
	}

	// Assumes the bucket layout defined inside of
	// https://github.com/coreos/fedora-coreos-tracker/issues/189
	err = api.UpdateBucketObjectsACL(spec.Bucket, filepath.Join("prod", "streams", specChannel, "builds", specVersion), specPolicy)
	if err != nil {
		plog.Fatalf("updating object ACLs: %v", err)
	}
}

func modifyReleaseMetadataIndex(spec *fcosChannelSpec, commitId string) {
	api, err := aws.New(&aws.Options{
		CredentialsFile: awsCredentialsFile,
		Profile:         spec.Profile,
		Region:          spec.Region,
	})
	if err != nil {
		plog.Fatalf("creating aws client: %v", err)
	}

	// Note we use S3 directly here instead of
	// FetchAndParseCanonicalReleaseIndex(), since that one uses the
	// CloudFronted URL and we need to be sure we're operating on the latest
	// version.  Plus we need S3 creds anyway later on to push the modified
	// release index back.

	path := filepath.Join("prod", "streams", specChannel, "releases.json")
	data, err := func() ([]byte, error) {
		f, err := api.DownloadFile(spec.Bucket, path)
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

	var releaseIdx fcos.ReleaseIndex
	err = json.Unmarshal(data, &releaseIdx)
	if err != nil {
		plog.Fatalf("unmarshaling release metadata json: %v", err)
	}

	releasePath := filepath.Join("prod", "streams", specChannel, "builds", specVersion, "release.json")
	url, err := url.Parse(fmt.Sprintf("https://builds.coreos.fedoraproject.org/%s", releasePath))
	if err != nil {
		plog.Fatalf("creating metadata url: %v", err)
	}

	releaseFile, err := api.DownloadFile(spec.Bucket, releasePath)
	if err != nil {
		plog.Fatalf("downloading release metadata at %s: %v", releasePath, err)
	}
	defer releaseFile.Close()

	releaseData, err := ioutil.ReadAll(releaseFile)
	if err != nil {
		plog.Fatalf("reading release metadata: %v", err)
	}

	var release fcos.Release
	err = json.Unmarshal(releaseData, &release)
	if err != nil {
		plog.Fatalf("unmarshaling release metadata: %v", err)
	}

	var commits []fcos.ReleaseCommit
	for arch, vals := range release.Architectures {
		commits = append(commits, fcos.ReleaseCommit{
			Architecture: arch,
			Checksum:     vals.Commit,
		})
	}

	newIdxRelease := fcos.ReleaseIndexRelease{
		CommitHash: commits,
		Version:    specVersion,
		Endpoint:   url.String(),
	}

	for i, rel := range releaseIdx.Releases {
		if compareStaticReleaseInfo(rel, newIdxRelease) {
			if i != (len(releaseIdx.Releases) - 1) {
				plog.Fatalf("build is already present and is not the latest release")
			}

			comp := compareCommits(rel.CommitHash, newIdxRelease.CommitHash)
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

	for _, archs := range release.Architectures {
		for name, media := range archs.Media {
			if name == "aws" {
				for region, ami := range media.Images {
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
		}
	}

	releaseIdx.Releases = append(releaseIdx.Releases, newIdxRelease)

	releaseIdx.Metadata.LastModified = time.Now().UTC().Format("2006-01-02T15:04:05Z")
	releaseIdx.Note = "For use only by Fedora CoreOS internal tooling.  All other applications should obtain release info from stream metadata endpoints."
	releaseIdx.Stream = specChannel

	out, err := json.Marshal(releaseIdx)
	if err != nil {
		plog.Fatalf("marshalling release metadata json: %v", err)
	}

	// we don't want this to be cached for very long so that e.g. Cincinnati picks it up quickly
	var releases_max_age = 60 * 5
	err = api.UploadObjectExt(bytes.NewReader(out), spec.Bucket, path, true, specPolicy, aws.ContentTypeJSON, releases_max_age)
	if err != nil {
		plog.Fatalf("uploading release metadata json: %v", err)
	}
}

func compareStaticReleaseInfo(a, b fcos.ReleaseIndexRelease) bool {
	if a.Version != b.Version || a.Endpoint != b.Endpoint {
		return false
	}
	return true
}

// returns -1 if a is a subset of b, 0 if equal, 1 if a is not a subset of b
func compareCommits(a, b []fcos.ReleaseCommit) int {
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
