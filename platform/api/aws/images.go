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
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/iam"
)

type EC2ImageType string

const (
	EC2ImageTypeHVM EC2ImageType = "hvm"
	EC2ImageTypePV  EC2ImageType = "paravirtual"
)

type EC2ImageFormat string

const (
	EC2ImageFormatRaw  EC2ImageFormat = ec2.DiskImageFormatRaw
	EC2ImageFormatVmdk                = ec2.DiskImageFormatVmdk
)

// TODO, these can be derived at runtime
// these are pv-grub-hd0_1.04-x86_64
var akis = map[string]string{
	"us-east-1":      "aki-919dcaf8",
	"us-east-2":      "aki-da055ebf",
	"us-west-1":      "aki-880531cd",
	"us-west-2":      "aki-fc8f11cc",
	"eu-west-1":      "aki-52a34525",
	"eu-west-2":      "aki-8b6369ef",
	"eu-central-1":   "aki-184c7a05",
	"ap-south-1":     "aki-a7305ac8",
	"ap-southeast-1": "aki-503e7402",
	"ap-southeast-2": "aki-c362fff9",
	"ap-northeast-1": "aki-176bf516",
	"ap-northeast-2": "aki-01a66b6f",
	"sa-east-1":      "aki-5553f448",
	"ca-central-1":   "aki-320ebd56",

	"us-gov-west-1": "aki-1de98d3e",
}

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

// Look up a Snapshot by name. Return nil if not found.
func (a *API) FindSnapshot(imageName string) (*Snapshot, error) {
	// Look for an existing snapshot with this image name.
	snapshotRes, err := a.ec2.DescribeSnapshots(&ec2.DescribeSnapshotsInput{
		Filters: []*ec2.Filter{
			&ec2.Filter{
				Name:   aws.String("status"),
				Values: aws.StringSlice([]string{"completed"}),
			},
			&ec2.Filter{
				Name:   aws.String("tag:Name"),
				Values: aws.StringSlice([]string{imageName}),
			},
		},
		OwnerIds: aws.StringSlice([]string{"self"}),
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
	taskRes, err := a.ec2.DescribeImportSnapshotTasks(&ec2.DescribeImportSnapshotTasksInput{})
	if err != nil {
		return nil, fmt.Errorf("unable to describe import tasks: %v", err)
	}
	for _, task := range taskRes.ImportSnapshotTasks {
		if *task.Description != imageName {
			continue
		}
		switch *task.SnapshotTaskDetail.Status {
		case "cancelled", "cancelling", "deleted", "deleting":
			continue
		case "completed":
			// Either we lost the race with a snapshot that just
			// completed or this is an old import task for a
			// snapshot that's been deleted. Check it.
			_, err := a.ec2.DescribeSnapshots(&ec2.DescribeSnapshotsInput{
				SnapshotIds: []*string{task.SnapshotTaskDetail.SnapshotId},
			})
			if err != nil {
				if awserr, ok := err.(awserr.Error); ok && awserr.Code() == "InvalidSnapshot.NotFound" {
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

	importRes, err := a.ec2.ImportSnapshot(&ec2.ImportSnapshotInput{
		RoleName:    aws.String(vmImportRole),
		Description: aws.String(imageName),
		DiskContainer: &ec2.SnapshotDiskContainer{
			// TODO(euank): allow s3 source / local file -> s3 source
			UserBucket: &ec2.UserBucket{
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
// tags), and return a Snapshot.
func (a *API) finishSnapshotTask(snapshotTaskID, imageName string) (*Snapshot, error) {
	snapshotDone := func(snapshotTaskID string) (bool, string, error) {
		taskRes, err := a.ec2.DescribeImportSnapshotTasks(&ec2.DescribeImportSnapshotTasksInput{
			ImportTaskIds: []*string{aws.String(snapshotTaskID)},
		})
		if err != nil {
			return false, "", err
		}

		details := taskRes.ImportSnapshotTasks[0].SnapshotTaskDetail

		// I dream of AWS specifying this as an enum shape, not string
		switch *details.Status {
		case "completed":
			return true, *details.SnapshotId, nil
		case "pending", "active":
			plog.Debugf("waiting for import task: %v (%v): %v", *details.Status, *details.Progress, *details.StatusMessage)
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
	iamc := iam.New(a.session)
	_, err := iamc.GetRole(&iam.GetRoleInput{
		RoleName: &vmImportRole,
	})
	if err != nil {
		if awserr, ok := err.(awserr.Error); ok && awserr.Code() == "NoSuchEntity" {
			// Role does not exist, let's try to create it
			_, err := iamc.CreateRole(&iam.CreateRoleInput{
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
	_, err = iamc.GetRolePolicy(&iam.GetRolePolicyInput{
		RoleName:   &vmImportRole,
		PolicyName: &policyName,
	})
	if err != nil {
		if awserr, ok := err.(awserr.Error); ok && awserr.Code() == "NoSuchEntity" {
			// Policy does not exist, let's try to create it
			_, err := iamc.PutRolePolicy(&iam.PutRolePolicyInput{
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
			"arn:aws:s3:::` + bucket + `",
			"arn:aws:s3:::` + bucket + `/*"
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

func (a *API) CreateHVMImage(snapshotID string, name string, description string) (string, error) {
	params := registerImageParams(snapshotID, name, description, "xvd", EC2ImageTypeHVM)
	params.EnaSupport = aws.Bool(true)
	params.SriovNetSupport = aws.String("simple")
	return a.createImage(params)
}

func (a *API) CreatePVImage(snapshotID string, name string, description string) (string, error) {
	params := registerImageParams(snapshotID, name, description, "sd", EC2ImageTypePV)
	params.KernelId = aws.String(akis[a.opts.Region])
	return a.createImage(params)
}

func (a *API) createImage(params *ec2.RegisterImageInput) (string, error) {
	res, err := a.ec2.RegisterImage(params)

	if err == nil {
		return *res.ImageId, nil
	}
	if awserr, ok := err.(awserr.Error); ok && awserr.Code() == "InvalidAMIName.Duplicate" {
		// The AMI already exists. Get its ID. Due to races, this
		// may take several attempts.
		for {
			imageID, err := a.FindImage(*params.Name)
			if err != nil {
				return "", err
			}
			if imageID != "" {
				plog.Infof("found existing image %v, reusing", imageID)
				return imageID, nil
			}
			plog.Debugf("failed to locate image %q, retrying...", *params.Name)
			time.Sleep(10 * time.Second)
		}
	}
	return "", fmt.Errorf("error creating AMI: %v", err)
}

const diskSize = 8 // GB

func registerImageParams(snapshotID, name, description string, diskBaseName string, imageType EC2ImageType) *ec2.RegisterImageInput {
	return &ec2.RegisterImageInput{
		Name:               aws.String(name),
		Description:        aws.String(description),
		Architecture:       aws.String("x86_64"),
		VirtualizationType: aws.String(string(imageType)),
		RootDeviceName:     aws.String(fmt.Sprintf("/dev/%sa", diskBaseName)),
		BlockDeviceMappings: []*ec2.BlockDeviceMapping{
			&ec2.BlockDeviceMapping{
				DeviceName: aws.String(fmt.Sprintf("/dev/%sa", diskBaseName)),
				Ebs: &ec2.EbsBlockDevice{
					SnapshotId:          aws.String(snapshotID),
					DeleteOnTermination: aws.Bool(true),
					VolumeSize:          aws.Int64(diskSize),
				},
			},
			&ec2.BlockDeviceMapping{
				DeviceName:  aws.String(fmt.Sprintf("/dev/%sb", diskBaseName)),
				VirtualName: aws.String("ephemeral0"),
			},
		},
	}
}

func (a *API) GrantLaunchPermission(imageID string, userIDs []string) error {
	arg := &ec2.ModifyImageAttributeInput{
		Attribute:        aws.String("launchPermission"),
		ImageId:          aws.String(imageID),
		LaunchPermission: &ec2.LaunchPermissionModifications{},
	}
	for _, userID := range userIDs {
		arg.LaunchPermission.Add = append(arg.LaunchPermission.Add, &ec2.LaunchPermission{
			UserId: aws.String(userID),
		})
	}
	_, err := a.ec2.ModifyImageAttribute(arg)
	if err != nil {
		return fmt.Errorf("couldn't grant launch permission: %v", err)
	}
	return nil
}

func (a *API) CopyImage(sourceImageID string, regions []string) (map[string]string, error) {
	type result struct {
		region  string
		imageID string
		err     error
	}

	image, err := a.describeImage(sourceImageID)
	if err != nil {
		return nil, err
	}

	describeSnapshotRes, err := a.ec2.DescribeSnapshots(&ec2.DescribeSnapshotsInput{
		SnapshotIds: []*string{image.BlockDeviceMappings[0].Ebs.SnapshotId},
	})
	if err != nil {
		return nil, fmt.Errorf("couldn't describe snapshot: %v", err)
	}
	snapshot := describeSnapshotRes.Snapshots[0]

	describeAttributeRes, err := a.ec2.DescribeImageAttribute(&ec2.DescribeImageAttributeInput{
		Attribute: aws.String("launchPermission"),
		ImageId:   aws.String(sourceImageID),
	})
	if err != nil {
		return nil, fmt.Errorf("couldn't describe launch permissions: %v", err)
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
			res.imageID, res.err = aa.copyImageIn(a.opts.Region, sourceImageID,
				*image.Name, *image.Description,
				image.Tags, snapshot.Tags,
				launchPermissions)
			ch <- res
		}()
	}
	wg.Wait()
	close(ch)

	amis := make(map[string]string)
	for res := range ch {
		if res.imageID != "" {
			amis[res.region] = res.imageID
		}
		if err == nil {
			err = res.err
		}
	}
	return amis, err
}

func (a *API) copyImageIn(sourceRegion, sourceImageID, name, description string, imageTags, snapshotTags []*ec2.Tag, launchPermissions []*ec2.LaunchPermission) (string, error) {
	imageID, err := a.FindImage(name)
	if err != nil {
		return "", err
	}

	if imageID == "" {
		copyRes, err := a.ec2.CopyImage(&ec2.CopyImageInput{
			SourceRegion:  aws.String(sourceRegion),
			SourceImageId: aws.String(sourceImageID),
			Name:          aws.String(name),
			Description:   aws.String(description),
		})
		if err != nil {
			return "", fmt.Errorf("couldn't initiate image copy to %v: %v", a.opts.Region, err)
		}
		imageID = *copyRes.ImageId
	}

	err = a.ec2.WaitUntilImageAvailable(&ec2.DescribeImagesInput{
		ImageIds: aws.StringSlice([]string{imageID}),
	})
	if err != nil {
		return "", fmt.Errorf("couldn't copy image to %v: %v", a.opts.Region, err)
	}

	if len(imageTags) > 0 {
		_, err = a.ec2.CreateTags(&ec2.CreateTagsInput{
			Resources: aws.StringSlice([]string{imageID}),
			Tags:      imageTags,
		})
		if err != nil {
			return "", fmt.Errorf("couldn't create image tags: %v", err)
		}
	}

	if len(snapshotTags) > 0 {
		image, err := a.describeImage(imageID)
		if err != nil {
			return "", err
		}
		_, err = a.ec2.CreateTags(&ec2.CreateTagsInput{
			Resources: []*string{image.BlockDeviceMappings[0].Ebs.SnapshotId},
			Tags:      snapshotTags,
		})
		if err != nil {
			return "", fmt.Errorf("couldn't create snapshot tags: %v", err)
		}
	}

	if len(launchPermissions) > 0 {
		_, err = a.ec2.ModifyImageAttribute(&ec2.ModifyImageAttributeInput{
			Attribute: aws.String("launchPermission"),
			ImageId:   aws.String(imageID),
			LaunchPermission: &ec2.LaunchPermissionModifications{
				Add: launchPermissions,
			},
		})
		if err != nil {
			return "", fmt.Errorf("couldn't grant launch permissions: %v", err)
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
		return "", fmt.Errorf("checking for duplicate images: %v", err)
	}

	return imageID, nil
}

// Find an image we own with the specified name. Return ID or "".
func (a *API) FindImage(name string) (string, error) {
	describeRes, err := a.ec2.DescribeImages(&ec2.DescribeImagesInput{
		Filters: []*ec2.Filter{
			&ec2.Filter{
				Name:   aws.String("name"),
				Values: aws.StringSlice([]string{name}),
			},
		},
		Owners: aws.StringSlice([]string{"self"}),
	})
	if err != nil {
		return "", fmt.Errorf("couldn't describe images: %v", err)
	}
	if len(describeRes.Images) > 1 {
		return "", fmt.Errorf("found multiple images with name %v", name)
	}
	if len(describeRes.Images) == 1 {
		return *describeRes.Images[0].ImageId, nil
	}
	return "", nil
}

func (a *API) describeImage(imageID string) (*ec2.Image, error) {
	describeRes, err := a.ec2.DescribeImages(&ec2.DescribeImagesInput{
		ImageIds: aws.StringSlice([]string{imageID}),
	})
	if err != nil {
		return nil, fmt.Errorf("couldn't describe image: %v", err)
	}
	return describeRes.Images[0], nil
}

// Grant everyone launch permission on the specified image and create-volume
// permission on its underlying snapshot.
func (a *API) PublishImage(imageID string) error {
	// snapshot create-volume permission
	image, err := a.describeImage(imageID)
	if err != nil {
		return err
	}
	snapshotID := image.BlockDeviceMappings[0].Ebs.SnapshotId
	_, err = a.ec2.ModifySnapshotAttribute(&ec2.ModifySnapshotAttributeInput{
		Attribute:  aws.String("createVolumePermission"),
		SnapshotId: snapshotID,
		CreateVolumePermission: &ec2.CreateVolumePermissionModifications{
			Add: []*ec2.CreateVolumePermission{
				&ec2.CreateVolumePermission{
					Group: aws.String("all"),
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("couldn't grant create volume permission on %v: %v", snapshotID, err)
	}

	// image launch permission
	_, err = a.ec2.ModifyImageAttribute(&ec2.ModifyImageAttributeInput{
		Attribute: aws.String("launchPermission"),
		ImageId:   aws.String(imageID),
		LaunchPermission: &ec2.LaunchPermissionModifications{
			Add: []*ec2.LaunchPermission{
				&ec2.LaunchPermission{
					Group: aws.String("all"),
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("couldn't grant launch permission on %v: %v", imageID, err)
	}

	return nil
}
