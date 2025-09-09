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

package aws

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/IBM/ibm-cos-sdk-go/aws/endpoints"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/smithy-go"

	"github.com/coreos/coreos-assembler/mantle/util"
)

// The default size of Container Linux disks on AWS, in GiB. See discussion in
// https://github.com/coreos/mantle/pull/944
const ContainerLinuxDiskSizeGiB = 8

type EC2ImageFormat string

const (
	EC2ImageFormatRaw  EC2ImageFormat = "RAW"
	EC2ImageFormatVmdk EC2ImageFormat = "VMDK"
)

func (e *EC2ImageFormat) Set(s string) error {
	switch s {
	case string(EC2ImageFormatVmdk):
		*e = EC2ImageFormatVmdk
	case string(EC2ImageFormatRaw):
		*e = EC2ImageFormatRaw
	default:
		return fmt.Errorf("invalid ec2 image format: must be raw or vmdk")
	}
	return nil
}

func (e *EC2ImageFormat) String() string {
	return string(*e)
}

func (e *EC2ImageFormat) Type() string {
	return "ec2ImageFormat"
}

var vmImportRole = "vmimport"

type Snapshot struct {
	SnapshotID string
}

type ImageData struct {
	AMI        string `json:"ami"`
	SnapshotID string `json:"snapshot"`
}

// Look up a Snapshot by name. Return nil if not found.
func (a *API) FindSnapshot(imageName string) (*Snapshot, error) {
	// Look for an existing snapshot with this image name.
	snapshotRes, err := a.ec2.DescribeSnapshots(context.Background(), &ec2.DescribeSnapshotsInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("status"),
				Values: []string{"completed"},
			},
			{
				Name:   aws.String("tag:Name"),
				Values: []string{imageName},
			},
		},
		OwnerIds: []string{"self"},
	})
	if err != nil {
		return nil, fmt.Errorf("unable to describe snapshots: %v", err)
	}
	if len(snapshotRes.Snapshots) > 1 {
		return nil, fmt.Errorf("found multiple matching snapshots")
	}
	if len(snapshotRes.Snapshots) == 1 {
		snapshotID := *snapshotRes.Snapshots[0].SnapshotId
		plog.Infof("found existing snapshot %v", snapshotID)
		return &Snapshot{
			SnapshotID: snapshotID,
		}, nil
	}

	// Look for an existing import task with this image name. We have
	// to fetch all of them and walk the list ourselves.
	var snapshotTaskID string
	taskRes, err := a.ec2.DescribeImportSnapshotTasks(context.Background(), &ec2.DescribeImportSnapshotTasksInput{})
	if err != nil {
		return nil, fmt.Errorf("unable to describe import tasks: %v", err)
	}
	for _, task := range taskRes.ImportSnapshotTasks {
		if task.Description == nil || *task.Description != imageName {
			continue
		}
		switch *task.SnapshotTaskDetail.Status {
		case "cancelled", "cancelling", "deleted", "deleting":
			continue
		case "completed":
			// Either we lost the race with a snapshot that just
			// completed or this is an old import task for a
			// snapshot that's been deleted. Check it.
			_, err := a.ec2.DescribeSnapshots(context.Background(), &ec2.DescribeSnapshotsInput{
				SnapshotIds: []string{*task.SnapshotTaskDetail.SnapshotId},
			})
			if err != nil {
				var ae smithy.APIError
				if errors.As(err, &ae) && ae.ErrorCode() == "InvalidSnapshot.NotFound" {
					continue
				} else {
					return nil, fmt.Errorf("couldn't describe snapshot from import task: %v", err)
				}
			}
		}
		if snapshotTaskID != "" {
			return nil, fmt.Errorf("found multiple matching import tasks")
		}
		snapshotTaskID = *task.ImportTaskId
	}
	if snapshotTaskID == "" {
		return nil, nil
	}

	plog.Infof("found existing snapshot import task %v", snapshotTaskID)
	return a.finishSnapshotTask(snapshotTaskID, imageName)
}

// CreateSnapshot creates an AWS Snapshot
func (a *API) CreateSnapshot(imageName, sourceURL string, format EC2ImageFormat) (*Snapshot, error) {
	if format == "" {
		format = EC2ImageFormatVmdk
	}
	s3url, err := url.Parse(sourceURL)
	if err != nil {
		return nil, err
	}
	if s3url.Scheme != "s3" {
		return nil, fmt.Errorf("source must have a 's3://' scheme, not: '%v://'", s3url.Scheme)
	}
	s3key := strings.TrimPrefix(s3url.Path, "/")

	importRes, err := a.ec2.ImportSnapshot(context.Background(), &ec2.ImportSnapshotInput{
		RoleName:    aws.String(vmImportRole),
		Description: aws.String(imageName),
		DiskContainer: &ec2types.SnapshotDiskContainer{
			// TODO(euank): allow s3 source / local file -> s3 source
			UserBucket: &ec2types.UserBucket{
				S3Bucket: aws.String(s3url.Host),
				S3Key:    aws.String(s3key),
			},
			Format: aws.String(string(format)),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("unable to create import snapshot task: %v", err)
	}

	plog.Infof("created snapshot import task %v", *importRes.ImportTaskId)
	return a.finishSnapshotTask(*importRes.ImportTaskId, imageName)
}

// Wait on a snapshot import task, post-process the snapshot (e.g. adding
// tags), and return a Snapshot. See also similar code in aliyun's
// finishImportImageTask.
func (a *API) finishSnapshotTask(snapshotTaskID, imageName string) (*Snapshot, error) {
	snapshotDone := func(snapshotTaskID string) (bool, string, error) {
		taskRes, err := a.ec2.DescribeImportSnapshotTasks(context.Background(), &ec2.DescribeImportSnapshotTasksInput{
			ImportTaskIds: []string{snapshotTaskID},
		})
		if err != nil {
			return false, "", err
		}

		details := taskRes.ImportSnapshotTasks[0].SnapshotTaskDetail

		if details == nil || details.Status == nil {
			plog.Debugf("waiting for import task; no details provided")
			return false, "", nil
		}

		// I dream of AWS specifying this as an enum shape, not string
		switch *details.Status {
		case "completed":
			return true, *details.SnapshotId, nil
		case "pending", "active":
			if details.Progress != nil && details.StatusMessage != nil {
				plog.Debugf("waiting for import task: %v (%v): %v", *details.Status, *details.Progress, *details.StatusMessage)
			} else {
				plog.Debugf("waiting for import task: %v", *details.Status)
			}
			return false, "", nil
		case "cancelled", "cancelling":
			return false, "", fmt.Errorf("import task cancelled")
		case "deleted", "deleting":
			errMsg := "unknown error occured importing snapshot"
			if details.StatusMessage != nil {
				errMsg = *details.StatusMessage
			}
			return false, "", fmt.Errorf("could not import snapshot: %v", errMsg)
		default:
			return false, "", fmt.Errorf("unexpected status: %v", *details.Status)
		}
	}

	// TODO(euank): write a waiter for import snapshot
	var snapshotID string
	for {
		var done bool
		var err error
		done, snapshotID, err = snapshotDone(snapshotTaskID)
		if err != nil {
			return nil, err
		}
		if done {
			break
		}
		time.Sleep(20 * time.Second)
	}

	// post-process
	err := a.CreateTags([]string{snapshotID}, map[string]string{
		"Name": imageName,
	})
	if err != nil {
		return nil, fmt.Errorf("couldn't create tags: %v", err)
	}

	return &Snapshot{
		SnapshotID: snapshotID,
	}, nil
}

func (a *API) CreateImportRole(bucket string) error {
	_, err := a.iam.GetRole(context.Background(), &iam.GetRoleInput{
		RoleName: &vmImportRole,
	})
	if err != nil {
		var ae smithy.APIError
		if errors.As(err, &ae) && ae.ErrorCode() == "NoSuchEntity" {
			// Role does not exist, let's try to create it
			_, err := a.iam.CreateRole(context.Background(), &iam.CreateRoleInput{
				RoleName: &vmImportRole,
				AssumeRolePolicyDocument: aws.String(`{
					"Version": "2012-10-17",
					"Statement": [{
						"Effect": "Allow",
						"Condition": {
							"StringEquals": {
								"sts:ExternalId": "vmimport"
							}
						},
						"Action": "sts:AssumeRole",
						"Principal": {
							"Service": "vmie.amazonaws.com"
						}
					}]
				}`),
			})
			if err != nil {
				return fmt.Errorf("coull not create vmimport role: %v", err)
			}
		}
	}

	// by convention, name our policies after the bucket so we can identify
	// whether a regional bucket is covered by a policy without parsing the
	// policy-doc json
	policyName := bucket
	_, err = a.iam.GetRolePolicy(context.Background(), &iam.GetRolePolicyInput{
		RoleName:   &vmImportRole,
		PolicyName: &policyName,
	})
	if err != nil {
		var ae smithy.APIError
		if errors.As(err, &ae) && ae.ErrorCode() == "NoSuchEntity" {
			// Policy does not exist, let's try to create it
			partition, ok := endpoints.PartitionForRegion(endpoints.DefaultPartitions(), a.opts.Region)
			if !ok {
				return fmt.Errorf("could not find partition for %v out of partitions %v", a.opts.Region, endpoints.DefaultPartitions())
			}
			_, err := a.iam.PutRolePolicy(context.Background(), &iam.PutRolePolicyInput{
				RoleName:   &vmImportRole,
				PolicyName: &policyName,
				PolicyDocument: aws.String((`{
	"Version": "2012-10-17",
	"Statement": [{
		"Effect": "Allow",
		"Action": [
			"s3:ListBucket",
			"s3:GetBucketLocation",
			"s3:GetObject"
		],
		"Resource": [
			"arn:` + partition.ID() + `:s3:::` + bucket + `",
			"arn:` + partition.ID() + `:s3:::` + bucket + `/*"
		]
	},
	{
		"Effect": "Allow",
		"Action": [
			"ec2:ModifySnapshotAttribute",
			"ec2:CopySnapshot",
			"ec2:RegisterImage",
			"ec2:Describe*"
		],
		"Resource": "*"
	}]
}`)),
			})
			if err != nil {
				return fmt.Errorf("could not create role policy: %v", err)
			}
		} else {
			return err
		}
	}

	return nil
}

func (a *API) CreateHVMImage(snapshotID string, diskSizeGiB uint, name string, description string, architecture string, volumetype string, imdsv2Only bool, X86BootMode string, billingCode string) (string, error) {
	var awsArch string
	var bootmode string
	if architecture == "" {
		architecture = runtime.GOARCH
	}
	switch architecture {
	case "amd64", "x86_64":
		awsArch = string(ec2types.ArchitectureTypeX8664)
		bootmode = X86BootMode
	case "arm64", "aarch64":
		awsArch = string(ec2types.ArchitectureTypeArm64)
		bootmode = "uefi"
	default:
		return "", fmt.Errorf("unsupported ec2 architecture %q", architecture)
	}

	// default to gp3
	if volumetype == "" {
		volumetype = "gp3"
	}
	params := &ec2.RegisterImageInput{
		Name:               aws.String(name),
		Description:        aws.String(description),
		Architecture:       ec2types.ArchitectureValues(awsArch),
		VirtualizationType: aws.String("hvm"),
		RootDeviceName:     aws.String("/dev/xvda"),
		BlockDeviceMappings: []ec2types.BlockDeviceMapping{
			{
				DeviceName: aws.String("/dev/xvda"),
				Ebs: &ec2types.EbsBlockDevice{
					SnapshotId:          aws.String(snapshotID),
					DeleteOnTermination: aws.Bool(true),
					VolumeSize:          aws.Int32(int32(diskSizeGiB)),
					VolumeType:          ec2types.VolumeType(volumetype),
				},
			},
			{
				DeviceName:  aws.String("/dev/xvdb"),
				VirtualName: aws.String("ephemeral0"),
			},
		},
		EnaSupport:      aws.Bool(true),
		SriovNetSupport: aws.String("simple"),
		BootMode:        ec2types.BootModeValues(bootmode),
	}
	if imdsv2Only {
		params.ImdsSupport = ec2types.ImdsSupportValues("v2.0")
	}
	// Set the billing product code for this AMI, if provided. The account must be
	// authorized by AWS to specify billing product codes. This is used by the
	// Windows License Included creation workflow to set the Windows LI billing
	// product code on an AMI.
	if billingCode != "" {
		params.BillingProducts = []string{billingCode}
	}

	return a.createImage(params)
}

func (a *API) deregisterImageIfExists(name string) error {
	imageID, err := a.FindImage(name)
	if err != nil {
		return err
	}
	if imageID != "" {
		_, err := a.ec2.DeregisterImage(context.Background(), &ec2.DeregisterImageInput{ImageId: &imageID})
		if err != nil {
			return err
		}
		plog.Infof("Deregistered existing image %s", imageID)
	}
	return nil
}

// Remove all uploaded data associated with an AMI.
func (a *API) RemoveImage(name string) error {
	// compatibility with old versions of this code, which created HVM
	// AMIs with an "-hvm" suffix
	err := a.deregisterImageIfExists(name + "-hvm")
	if err != nil {
		return err
	}
	err = a.deregisterImageIfExists(name)
	if err != nil {
		return err
	}

	snapshot, err := a.FindSnapshot(name)
	if err != nil {
		return err
	}
	if snapshot != nil {
		// We explicitly ignore errors here in case somehow another AMI was based
		// on that snapshot
		_, err := a.ec2.DeleteSnapshot(context.Background(), &ec2.DeleteSnapshotInput{SnapshotId: &snapshot.SnapshotID})
		if err != nil {
			plog.Warningf("deleting snapshot %s: %v", snapshot.SnapshotID, err)
		} else {
			plog.Infof("Deleted existing snapshot %s", snapshot.SnapshotID)
		}
	}

	return nil
}

func (a *API) createImage(params *ec2.RegisterImageInput) (string, error) {
	res, err := a.ec2.RegisterImage(context.Background(), params)

	var imageID string
	if err == nil {
		imageID = *res.ImageId
		plog.Infof("created image %v", imageID)
	} else {
		var ae smithy.APIError
		if errors.As(err, &ae) && ae.ErrorCode() == "InvalidAMIName.Duplicate" {
			// The AMI already exists. Get its ID. Due to races, this
			// may take several attempts.
			timeout := 5 * time.Minute
			delay := 10 * time.Second
			err = util.RetryUntilTimeout(timeout, delay, func() error {
				imageID, err = a.FindImage(*params.Name)
				if err != nil {
					return err
				}
				if imageID != "" {
					plog.Infof("found existing image %v, reusing", imageID)
					return nil
				}
				return fmt.Errorf("failed to locate image %q", *params.Name)
			})
			if err != nil {
				return "", fmt.Errorf("error finding duplicate image id: %v", err)
			}
		} else {
			return "", fmt.Errorf("error creating AMI: %v", err)
		}
	}

	// Attempt to tag inside of a retry loop; AWS eventual consistency means that just because
	// the FindImage call found the AMI it might not be found by the CreateTags call
	err = util.RetryConditional(6, 5*time.Second, func(err error) bool {
		var ae smithy.APIError
		if errors.As(err, &ae) && ae.ErrorCode() == "InvalidAMIID.NotFound" {
			return true
		}
		return false
	}, func() error {
		// We do this even in the already-exists path in case the previous
		// run was interrupted.
		return a.CreateTags([]string{imageID}, map[string]string{
			"Name": *params.Name,
		})
	})
	if err != nil {
		return "", fmt.Errorf("couldn't tag image name: %v", err)
	}

	return imageID, nil
}

// GrantVolumePermission grants permission to access an EC2 snapshot volume (referenced by its snapshot ID)
// to a list of AWS users (referenced by their 12-digit numerical user IDs).
func (a *API) GrantVolumePermission(snapshotID string, userIDs []string) error {
	arg := &ec2.ModifySnapshotAttributeInput{
		Attribute:              ec2types.SnapshotAttributeNameCreateVolumePermission,
		SnapshotId:             aws.String(snapshotID),
		CreateVolumePermission: &ec2types.CreateVolumePermissionModifications{},
	}
	for _, userID := range userIDs {
		arg.CreateVolumePermission.Add = append(arg.CreateVolumePermission.Add, ec2types.CreateVolumePermission{
			UserId: aws.String(userID),
		})
	}
	_, err := a.ec2.ModifySnapshotAttribute(context.Background(), arg)
	if err != nil {
		return fmt.Errorf("couldn't grant snapshot volume permission: %v", err)
	}
	return nil
}

func (a *API) GrantLaunchPermission(imageID string, userIDs []string) error {
	arg := &ec2.ModifyImageAttributeInput{
		Attribute:        aws.String("launchPermission"),
		ImageId:          aws.String(imageID),
		LaunchPermission: &ec2types.LaunchPermissionModifications{},
	}
	for _, userID := range userIDs {
		arg.LaunchPermission.Add = append(arg.LaunchPermission.Add, ec2types.LaunchPermission{
			UserId: aws.String(userID),
		})
	}
	_, err := a.ec2.ModifyImageAttribute(context.Background(), arg)
	if err != nil {
		return fmt.Errorf("couldn't grant launch permission: %v", err)
	}
	return nil
}

func (a *API) CopyImage(sourceImageID string, regions []string, cb func(string, ImageData)) error {
	type result struct {
		region string
		data   ImageData
		err    error
	}

	image, err := a.DescribeImage(sourceImageID)
	if err != nil {
		return err
	}

	snapshotID, err := getImageSnapshotID(image)
	if err != nil {
		return err
	}
	describeSnapshotRes, err := a.ec2.DescribeSnapshots(context.Background(), &ec2.DescribeSnapshotsInput{
		SnapshotIds: []string{snapshotID},
	})
	if err != nil {
		return fmt.Errorf("couldn't describe snapshot: %v", err)
	}
	snapshot := describeSnapshotRes.Snapshots[0]

	describeSnapshotAttributeRes, err := a.ec2.DescribeSnapshotAttribute(context.Background(), &ec2.DescribeSnapshotAttributeInput{
		Attribute:  ec2types.SnapshotAttributeNameCreateVolumePermission,
		SnapshotId: aws.String(snapshotID),
	})
	if err != nil {
		return fmt.Errorf("couldn't describe createVolumePermission: %v", err)
	}
	createVolumePermissions := describeSnapshotAttributeRes.CreateVolumePermissions

	describeAttributeRes, err := a.ec2.DescribeImageAttribute(context.Background(), &ec2.DescribeImageAttributeInput{
		Attribute: ec2types.ImageAttributeNameLaunchPermission,
		ImageId:   aws.String(sourceImageID),
	})
	if err != nil {
		return fmt.Errorf("couldn't describe launch permissions: %v", err)
	}
	launchPermissions := describeAttributeRes.LaunchPermissions

	var wg sync.WaitGroup
	ch := make(chan result, len(regions))
	for _, region := range regions {
		opts := *a.opts
		opts.Region = region
		aa, err := New(&opts)
		if err != nil {
			break
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := result{region: aa.opts.Region}
			res.data, res.err = aa.copyImageIn(a.opts.Region, sourceImageID,
				*image.Name, *image.Description,
				image.Tags, snapshot.Tags,
				launchPermissions, createVolumePermissions)
			ch <- res
		}()
	}
	go func() {
		wg.Wait()
		close(ch)
	}()

	for res := range ch {
		if res.data.AMI != "" {
			cb(res.region, res.data)
		}
		if err == nil {
			err = res.err
		}
	}

	return err
}

func (a *API) copyImageIn(sourceRegion, sourceImageID, name, description string, imageTags, snapshotTags []ec2types.Tag, launchPermissions []ec2types.LaunchPermission, createVolumePermissions []ec2types.CreateVolumePermission) (ImageData, error) {
	imageID, err := a.FindImage(name)
	if err != nil {
		return ImageData{}, err
	}

	if imageID == "" {
		copyRes, err := a.ec2.CopyImage(context.Background(), &ec2.CopyImageInput{
			SourceRegion:  aws.String(sourceRegion),
			SourceImageId: aws.String(sourceImageID),
			Name:          aws.String(name),
			Description:   aws.String(description),
		})
		if err != nil {
			return ImageData{}, fmt.Errorf("couldn't initiate image copy to %v: %v", a.opts.Region, err)
		}
		imageID = *copyRes.ImageId
	}

	// The 10-minute default timeout is not enough. Wait up to 30 minutes.
	waiter := ec2.NewImageAvailableWaiter(a.ec2)
	err = waiter.Wait(context.Background(), &ec2.DescribeImagesInput{
		ImageIds: []string{imageID},
	}, 30*time.Minute)
	if err != nil {
		return ImageData{}, fmt.Errorf("couldn't copy image to %v: %v", a.opts.Region, err)
	}

	if len(imageTags) > 0 {
		_, err = a.ec2.CreateTags(context.Background(), &ec2.CreateTagsInput{
			Resources: []string{imageID},
			Tags:      imageTags,
		})
		if err != nil {
			return ImageData{}, fmt.Errorf("couldn't create image tags: %v", err)
		}
	}

	image, err := a.DescribeImage(imageID)
	if err != nil {
		return ImageData{}, err
	}

	snapshotID, err := getImageSnapshotID(image)
	if err != nil {
		return ImageData{}, err
	}

	if len(snapshotTags) > 0 {
		_, err = a.ec2.CreateTags(context.Background(), &ec2.CreateTagsInput{
			Resources: []string{snapshotID},
			Tags:      snapshotTags,
		})
		if err != nil {
			return ImageData{}, fmt.Errorf("couldn't create snapshot tags: %v", err)
		}
	}

	if len(createVolumePermissions) > 0 {
		_, err = a.ec2.ModifySnapshotAttribute(context.Background(), &ec2.ModifySnapshotAttributeInput{
			Attribute:  ec2types.SnapshotAttributeNameCreateVolumePermission,
			SnapshotId: &snapshotID,
			CreateVolumePermission: &ec2types.CreateVolumePermissionModifications{
				Add: createVolumePermissions,
			},
		})
		if err != nil {
			return ImageData{}, fmt.Errorf("couldn't grant createVolumePermissions: %v", err)
		}
	}

	if len(launchPermissions) > 0 {
		_, err = a.ec2.ModifyImageAttribute(context.Background(), &ec2.ModifyImageAttributeInput{
			Attribute: aws.String("launchPermission"),
			ImageId:   aws.String(imageID),
			LaunchPermission: &ec2types.LaunchPermissionModifications{
				Add: launchPermissions,
			},
		})
		if err != nil {
			return ImageData{}, fmt.Errorf("couldn't grant launch permissions: %v", err)
		}
	}

	// The AMI created by CopyImage doesn't immediately appear in
	// DescribeImagesOutput, and CopyImage doesn't enforce the
	// constraint that multiple images cannot have the same name.
	// As a result we could have created a duplicate image after
	// losing a race with a CopyImage task created by a previous run.
	// Don't try to clean this up automatically for now, but at least
	// detect it so plume pre-release doesn't leave any surprises for
	// plume release.
	_, err = a.FindImage(name)
	if err != nil {
		return ImageData{}, fmt.Errorf("checking for duplicate images: %v", err)
	}

	return ImageData{
		AMI:        imageID,
		SnapshotID: snapshotID,
	}, nil
}

// Find an image we own with the specified name. Return ID or "".
func (a *API) FindImage(name string) (string, error) {
	describeRes, err := a.ec2.DescribeImages(context.Background(), &ec2.DescribeImagesInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("name"),
				Values: []string{name},
			},
		},
		Owners: []string{"self"},
	})
	if err != nil {
		return "", fmt.Errorf("couldn't describe images: %v", err)
	}
	if len(describeRes.Images) > 1 {
		return "", fmt.Errorf("found multiple images with name %v. DescribeImage output: %v", name, describeRes.Images)
	}
	if len(describeRes.Images) == 1 {
		return *describeRes.Images[0].ImageId, nil
	}
	return "", nil
}

// Deregisters the ami.
func (a *API) RemoveByAmiTag(imageID string, allowMissing bool) error {
	_, err := a.ec2.DeregisterImage(context.Background(), &ec2.DeregisterImageInput{ImageId: &imageID})
	if err != nil {
		if allowMissing {
			var ae smithy.APIError
			if errors.As(err, &ae) {
				if ae.ErrorCode() == "InvalidAMIID.NotFound" {
					plog.Infof("%s does not exist.", imageID)
					return nil
				}
				if ae.ErrorCode() == "InvalidAMIID.Unavailable" {
					plog.Infof("%s is no longer available.", imageID)
					return nil
				}
			}
		}
		return err
	}
	plog.Infof("Deregistered existing AMI %s", imageID)
	return nil
}

func (a *API) RemoveBySnapshotTag(snapshotID string, allowMissing bool) error {
	_, err := a.ec2.DeleteSnapshot(context.Background(), &ec2.DeleteSnapshotInput{SnapshotId: &snapshotID})
	if err != nil {
		if allowMissing {
			var ae smithy.APIError
			if errors.As(err, &ae) {
				if ae.ErrorCode() == "InvalidSnapshot.NotFound" {
					plog.Infof("%s does not exist.", snapshotID)
					return nil
				}
				if ae.ErrorCode() == "InvalidSnapshot.Unavailable" {
					plog.Infof("%s is no longer available.", snapshotID)
					return nil
				}
			}
		}
		return err
	}
	plog.Infof("Deregistered existing snapshot %s", snapshotID)
	return nil
}

func (a *API) DescribeImage(imageID string) (*ec2types.Image, error) {
	describeRes, err := a.ec2.DescribeImages(context.Background(), &ec2.DescribeImagesInput{
		ImageIds: []string{imageID},
	})
	if err != nil {
		return nil, fmt.Errorf("couldn't describe image: %v", err)
	}
	return &describeRes.Images[0], nil
}

// Grant everyone launch permission on the specified image and create-volume
// permission on its underlying snapshot.
func (a *API) PublishImage(imageID string) error {
	// snapshot create-volume permission
	image, err := a.DescribeImage(imageID)
	if err != nil {
		return err
	}
	snapshotID, err := getImageSnapshotID(image)
	if err != nil {
		return err
	}
	_, err = a.ec2.ModifySnapshotAttribute(context.Background(), &ec2.ModifySnapshotAttributeInput{
		Attribute:  ec2types.SnapshotAttributeNameCreateVolumePermission,
		SnapshotId: &snapshotID,
		CreateVolumePermission: &ec2types.CreateVolumePermissionModifications{
			Add: []ec2types.CreateVolumePermission{
				{
					Group: ec2types.PermissionGroupAll,
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("couldn't grant create volume permission on %v: %v", snapshotID, err)
	}

	// image launch permission
	_, err = a.ec2.ModifyImageAttribute(context.Background(), &ec2.ModifyImageAttributeInput{
		Attribute: aws.String("launchPermission"),
		ImageId:   aws.String(imageID),
		LaunchPermission: &ec2types.LaunchPermissionModifications{
			Add: []ec2types.LaunchPermission{
				{
					Group: ec2types.PermissionGroupAll,
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("couldn't grant launch permission on %v: %v", imageID, err)
	}

	return nil
}

func getImageSnapshotID(image *ec2types.Image) (string, error) {
	// The EBS volume is usually listed before the ephemeral volume, but
	// not always, e.g. ami-fddb0490 or ami-8cd40ce1 in cn-north-1
	for _, mapping := range image.BlockDeviceMappings {
		if mapping.Ebs != nil {
			return *mapping.Ebs.SnapshotId, nil
		}
	}
	// We observed a case where a returned `image` didn't have a block
	// device mapping.  Hopefully retrying this a couple times will work
	// and it's just a sorta eventual consistency thing
	return "", fmt.Errorf("no backing block device for %v", image.ImageId)
}

func (a *API) FindSnapshotDiskSizeGiB(snapshotID string) (uint, error) {
	result, err := a.ec2.DescribeSnapshots(context.Background(), &ec2.DescribeSnapshotsInput{
		SnapshotIds: []string{snapshotID},
	})
	if err != nil {
		return 0, fmt.Errorf("failed to describe snapshot: %v", err)
	}

	if len(result.Snapshots) == 0 {
		return 0, fmt.Errorf("no snapshot found with ID %s", snapshotID)
	}

	return uint(aws.ToInt32(result.Snapshots[0].VolumeSize)), nil
}
