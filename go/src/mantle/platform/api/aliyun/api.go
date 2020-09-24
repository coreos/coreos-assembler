// Copyright 2019 Red Hat
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

package aliyun

import (
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/coreos/mantle/auth"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/pkg/capnslog"
	"github.com/coreos/pkg/multierror"

	"github.com/aliyun/alibaba-cloud-sdk-go/sdk/requests"
	"github.com/aliyun/alibaba-cloud-sdk-go/services/ecs"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

var plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "platform/api/aliyun")

type Options struct {
	*platform.Options
	// The aliyun region regional api calls should use
	Region string

	// Config file. Defaults to ~/.aliyun/config.json
	ConfigPath string
	// The profile to use when resolving credentials, if applicable
	Profile string

	// AccessKeyID is the optional access key to use. It will override all other sources
	AccessKeyID string
	// SecretKey is the optional secret key to use. It will override all other sources
	SecretKey string
}

type API struct {
	ecs  *ecs.Client
	oss  *oss.Client
	opts *Options
}

// New creates a new aliyun API wrapper. It uses credentials from any of the
// standard credentials sources, including the environment and the profile
// configured in ~/.aliyun.
func New(opts *Options) (*API, error) {
	profiles, err := auth.ReadAliyunConfig(opts.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("couldn't read aliyun config: %v", err)
	}

	if opts.Profile == "" {
		opts.Profile = "default"
	}

	profile, ok := profiles[opts.Profile]
	if !ok {
		return nil, fmt.Errorf("no such profile %q", opts.Profile)
	}

	if opts.AccessKeyID == "" {
		opts.AccessKeyID = profile.AccessKeyID
	}

	if opts.SecretKey == "" {
		opts.SecretKey = profile.AccessKeySecret
	}

	if opts.Region == "" {
		opts.Region = profile.Region
	}

	ecs, err := ecs.NewClientWithAccessKey(opts.Region, opts.AccessKeyID, opts.SecretKey)
	if err != nil {
		return nil, err
	}

	oss, err := oss.New(getOSSEndpoint(opts.Region), opts.AccessKeyID, opts.SecretKey)
	if err != nil {
		return nil, err
	}

	api := &API{
		ecs:  ecs,
		oss:  oss,
		opts: opts,
	}

	return api, nil
}

func getOSSEndpoint(region string) string {
	return fmt.Sprintf("https://oss-%s.aliyuncs.com", region)
}

// CopyImage replicates an image to a new region
func (a *API) CopyImage(source_id, dest_name, dest_region, dest_description, kms_key_id string, encrypted bool) (string, error) {
	request := ecs.CreateCopyImageRequest()
	request.Scheme = "https"
	request.ImageId = source_id
	request.DestinationImageName = dest_name
	request.DestinationRegionId = dest_region
	request.DestinationDescription = dest_description
	request.KMSKeyId = kms_key_id
	request.Encrypted = requests.NewBoolean(encrypted)
	request.Tag = &[]ecs.CopyImageTag{
		{
			Key:   "created-by",
			Value: "mantle",
		},
	}

	response, err := a.ecs.CopyImage(request)
	if err != nil {
		return "", fmt.Errorf("copying image: %v", err)
	}
	return response.ImageId, nil
}

// ImportImage attempts to import an image from OSS returning the image_id & error
//
// NOTE: this function will re-use existing images that share the same final name
// if the name is not unique then provide force to pre-remove any images with the
// specified name
func (a *API) ImportImage(format, bucket, object, image_size, device, name, description, architecture string, force bool) (string, error) {
	images, err := a.GetImages(name)
	if err != nil {
		return "", fmt.Errorf("getting images: %v", err)
	}

	for _, image := range images.Images.Image {
		if force {
			plog.Infof("deleting pre-existing image %v", image.ImageId)
			err = a.DeleteImage(image.ImageId, force)
			if err != nil {
				return "", fmt.Errorf("deleting image %v: %v", image.ImageId, err)
			}
		} else {
			// save time & re-use the existing image but inform the user
			plog.Infof("reusing existing image %v", image.ImageId)
			return image.ImageId, nil
		}
	}

	request := ecs.CreateImportImageRequest()
	request.Scheme = "https"
	request.DiskDeviceMapping = &[]ecs.ImportImageDiskDeviceMapping{
		{
			Format:        format,
			OSSBucket:     bucket,
			OSSObject:     object,
			DiskImageSize: image_size,
			Device:        device,
		},
	}
	request.ImageName = name
	request.Description = description
	request.Architecture = architecture

	plog.Infof("importing image")
	response, err := a.ecs.ImportImage(request)
	if err != nil {
		return "", fmt.Errorf("importing image: %v", err)
	}

	return a.finishImportImageTask(response)
}

// Wait for the import image task and return the image id. See also similar
// code in AWS' finishSnapshotTask.
func (a *API) finishImportImageTask(importImageResponse *ecs.ImportImageResponse) (string, error) {
	importDone := func(taskId string) (bool, error) {
		request := ecs.CreateDescribeTasksRequest()
		request.TaskIds = taskId
		res, err := a.ecs.DescribeTasks(request)
		if err != nil {
			return false, err
		}

		if len(res.TaskSet.Task) != 1 {
			return false, fmt.Errorf("expected result about one task, got %v", res.TaskSet.Task)
		}

		switch res.TaskSet.Task[0].TaskStatus {
		case "Finished":
			return true, nil
		case "Processing":
			return false, nil
		case "Waiting":
			return false, nil
		case "Deleted":
			return false, fmt.Errorf("task %v was deleted", taskId)
		case "Paused":
			return false, fmt.Errorf("task %v was paused", taskId)
		case "Failed":
			return false, fmt.Errorf("task %v failed", taskId)
		default:
			return false, fmt.Errorf("unexpected status for task %v: %v", taskId, res.TaskSet.Task[0].TaskStatus)
		}
	}

	for {
		done, err := importDone(importImageResponse.TaskId)
		if err != nil {
			return "", err
		}
		if done {
			break
		}
		time.Sleep(10 * time.Second)
	}

	return importImageResponse.ImageId, nil
}

// GetImages retrieves a list of images by ImageName
func (a *API) GetImages(name string) (*ecs.DescribeImagesResponse, error) {
	request := ecs.CreateDescribeImagesRequest()
	request.Scheme = "https"
	request.ImageName = name
	return a.ecs.DescribeImages(request)
}

// GetImagesByID retrieves a list of images by ImageId
func (a *API) GetImagesByID(id string) (*ecs.DescribeImagesResponse, error) {
	request := ecs.CreateDescribeImagesRequest()
	request.Scheme = "https"
	request.ImageId = id
	return a.ecs.DescribeImages(request)
}

// DeleteImage deletes an image and it's underlying snapshots
func (a *API) DeleteImage(id string, force bool) error {
	request := ecs.CreateDeleteImageRequest()
	request.Scheme = "https"
	request.ImageId = id
	request.Force = requests.NewBoolean(force)

	images, err := a.GetImagesByID(id)
	if err != nil {
		return fmt.Errorf("getting image: %v", err)
	}

	_, err = a.ecs.DeleteImage(request)
	if err != nil {
		return fmt.Errorf("deleting image: %v", err)
	}

	var errs multierror.Error
	for _, img := range images.Images.Image {
		for _, mapping := range img.DiskDeviceMappings.DiskDeviceMapping {
			err = a.DeleteSnapshot(mapping.SnapshotId, force)
			if err != nil {
				errs = append(errs, fmt.Errorf("deleting snapshot %v: %v", mapping.SnapshotId, err))
			}
		}
	}
	return errs.AsError()
}

// DeleteSnapshot deletes a snapshot
func (a *API) DeleteSnapshot(id string, force bool) error {
	request := ecs.CreateDeleteSnapshotRequest()
	request.Scheme = "https"
	request.SnapshotId = id
	request.Force = requests.NewBoolean(force)
	_, err := a.ecs.DeleteSnapshot(request)
	return err
}

// UploadFile is a multipart upload, use for larger files
//
// NOTE: this function will return early if an object already exists
// at the specified path, if it might not be unique provide the force
// option to skip these checks
func (a *API) UploadFile(filepath, bucket, path string, force bool) error {
	bucketClient, err := a.oss.Bucket(bucket)
	if err != nil {
		return fmt.Errorf("getting bucket %q: %v", bucket, err)
	}

	if !force {
		// TODO: Switch to head object whenever the library actually adds the call :(
		objects, err := bucketClient.ListObjects()
		if err != nil {
			return fmt.Errorf("listing objects in bucket: %v", err)
		}

		for _, object := range objects.Objects {
			// Already exists, inform & re-use
			if object.Key == path {
				plog.Infof("object already exists and force is false")
				return nil
			}
		}
	}

	// Use 1000K part size with 10 coroutines to speed up the upload
	plog.Infof("uploading oss://%v/%v", bucket, path)
	return bucketClient.UploadFile(path, filepath, 1000*1024, oss.Routines(10))
}

// DeleteFile deletes a file from an OSS bucket
func (a *API) DeleteFile(bucket, path string) error {
	bucketClient, err := a.oss.Bucket(bucket)
	if err != nil {
		return fmt.Errorf("getting bucket %q: %v", bucket, err)
	}

	plog.Infof("deleting oss://%v/%v", bucket, path)
	return bucketClient.DeleteObject(path)
}

// PutObject performs a singlepart upload into an OSS bucket
func (a *API) PutObject(r io.Reader, bucket, path string, force bool) error {
	bucketClient, err := a.oss.Bucket(bucket)
	if err != nil {
		return fmt.Errorf("getting bucket %q: %v", bucket, err)
	}

	if !force {
		// TODO: Switch to head object whenever the library actually adds the call :(
		objects, err := bucketClient.ListObjects()
		if err != nil {
			return fmt.Errorf("listing objects in bucket: %v", err)
		}

		for _, object := range objects.Objects {
			if object.Key == path {
				return fmt.Errorf("object already exists and force is false")
			}
		}
	}

	return bucketClient.PutObject(path, r)
}

// ListRegions lists the enabled regions in aliyun implicitly
// by the Profile and Region options.
func (a *API) ListRegions() ([]string, error) {
	request := ecs.CreateDescribeRegionsRequest()
	request.Scheme = "https"

	response, err := a.ecs.DescribeRegions(request)
	if err != nil {
		return nil, fmt.Errorf("describing regions: %v", err)
	}
	ret := make([]string, 0, len(response.Regions.Region))
	for _, region := range response.Regions.Region {
		ret = append(ret, region.RegionId)
	}
	sort.Strings(ret)
	return ret, nil
}
