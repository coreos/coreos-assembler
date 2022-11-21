// Copyright 2021 Red Hat
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

// Most of the functions here follow: https://github.com/ppc64le-cloud/pvsadm which is an implementation of
// tools to interact with the IBMCloud storage and also the Power Virtual Server.

package ibmcloud

import (
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/IBM-Cloud/bluemix-go/api/resource/resourcev2/controllerv2"
	"github.com/IBM/ibm-cos-sdk-go/aws"
	"github.com/IBM/ibm-cos-sdk-go/aws/awserr"
	"github.com/IBM/ibm-cos-sdk-go/aws/credentials/ibmiam"
	"github.com/IBM/ibm-cos-sdk-go/aws/session"
	"github.com/IBM/ibm-cos-sdk-go/service/s3"
	"github.com/IBM/ibm-cos-sdk-go/service/s3/s3manager"
)

// S3Client - to interface with the IBMCloud s3 storage
type S3Client struct {
	apiKey                  string
	objectStorageInstanceID string
	storageClass            string
	serviceEndpoint         string
	s3Session               *s3.S3
}

var (
	authEndpoint = "https://iam.cloud.ibm.com/identity/token"
)

// NewS3Client accepts apikey, instanceid of the IBM Cloud Object Storage instance and returns the s3 client
func (a *API) NewS3Client(cloudObjectStorageName, region string) (err error) {
	a.s3client = &S3Client{}
	var instanceID string
	objectStorageInstances, err := a.client.ResourceClientV2.ListInstances(controllerv2.ServiceInstanceQuery{
		Type: "service_instance",
		Name: cloudObjectStorageName,
	})
	if err != nil {
		return fmt.Errorf("failed to list the cloud object storage instances: %v", err)
	}
	found := false
	for _, instance := range objectStorageInstances {
		plog.Infof("Service ID: %s, region_id: %s, Name: %s", instance.Guid, instance.RegionID, instance.Name)
		if instance.Name == cloudObjectStorageName {
			instanceID = instance.Guid
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("instance: %s not found", cloudObjectStorageName)
	}
	a.s3client.objectStorageInstanceID = instanceID
	a.s3client.apiKey = a.opts.ApiKey
	a.s3client.serviceEndpoint = fmt.Sprintf("https://s3.%s.cloud-object-storage.appdomain.cloud", region)
	a.s3client.storageClass = fmt.Sprintf("%s-standard", region)
	conf := aws.NewConfig().
		WithRegion(a.s3client.storageClass).
		WithEndpoint(a.s3client.serviceEndpoint).
		WithCredentials(ibmiam.NewStaticCredentials(aws.NewConfig(), authEndpoint, a.s3client.apiKey, a.s3client.objectStorageInstanceID)).
		WithS3ForcePathStyle(true)

	// Create client connection
	sess := session.Must(session.NewSession()) // Creating a new session
	a.s3client.s3Session = s3.New(sess, conf)  // Creating a new client
	return nil
}

// CheckBucketExists will verify the existence of the bucket for the particular account in the particular cloud object storage instance
func (a *API) CheckBucketExists(bucketName string) (bool, error) {
	result, err := a.s3client.s3Session.ListBuckets(nil)
	if err != nil {
		plog.Infof("Unable to list buckets, %v\n", err)
		return false, err
	}

	bucketExists := false
	for _, b := range result.Buckets {
		if aws.StringValue(b.Name) == bucketName {
			bucketExists = true
		}
	}

	if bucketExists {
		return true, nil
	}
	return false, nil
}

// CreateBucket creates a new bucket in the provided cloud object storage instance
func (a *API) CreateBucket(bucketName string) error {
	plog.Infof("Creating bucket %q ...\n", bucketName)
	_, err := a.s3client.s3Session.CreateBucket(&s3.CreateBucketInput{
		Bucket: aws.String(bucketName), // New Bucket Name
	})
	if err != nil {
		return fmt.Errorf("Unable to create bucket %q, %v", bucketName, err)
	}
	// Wait until bucket is created before finishing
	plog.Infof("Waiting for bucket %q to be created...\n", bucketName)

	err = a.s3client.s3Session.WaitUntilBucketExists(&s3.HeadBucketInput{
		Bucket: aws.String(bucketName),
	})
	return err
}

func (a *API) checkIfObjectExists(objectName, bucketName string) bool {
	input := &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectName),
	}

	_, err := a.s3client.s3Session.GetObject(input)
	// XXX: this should actually check the exact error returned
	return err == nil
}

// UploadObject - upload to s3 bucket
func (a *API) UploadObject(r io.Reader, objectName, bucketName string, force bool) error {
	// check if image exists and force is not set then bail
	if !force {
		if a.checkIfObjectExists(objectName, bucketName) {
			plog.Infof("skipping upload since object exists and force was not set: %s  %s", objectName, bucketName)
			return nil
		}
	}

	plog.Infof("Uploading object %q ...\n", objectName)
	// Create an uploader with S3 client
	uploader := s3manager.NewUploaderWithClient(a.s3client.s3Session)

	// Upload input parameters
	upParams := &s3manager.UploadInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectName),
		Body:   r,
	}

	// Perform an upload.
	startTime := time.Now()
	result, err := uploader.Upload(upParams)
	if err != nil {
		return err
	}
	plog.Infof("Upload completed successfully in %f seconds to location %s\n", time.Since(startTime).Seconds(), result.Location)
	return err
}

// CopyObject - Copy an Object to a new location
func (a *API) CopyObject(srcBucket, srcName, destBucket string) error {
	_, err := a.s3client.s3Session.CopyObject(&s3.CopyObjectInput{
		CopySource: aws.String(url.QueryEscape(fmt.Sprintf("%s/%s", srcBucket, srcName))),
		Bucket:     aws.String(destBucket),
		Key:        aws.String(srcName),
	})
	if err != nil {
		if awserr, ok := err.(awserr.Error); ok {
			err = awserr
		}
		return fmt.Errorf("Error copying object to bucket: %v", err)
	}

	// Wait to see if the item got copied
	err = a.s3client.s3Session.WaitUntilObjectExists(&s3.HeadObjectInput{Bucket: aws.String(destBucket), Key: aws.String(srcName)})
	if err != nil {
		return fmt.Errorf("Error occurred while waiting for item %q to be copied to bucket %q, %v", srcName, destBucket, err)
	}

	plog.Infof("Item %q successfully copied from bucket %q to bucket %q\n", srcName, srcBucket, destBucket)
	return err
}
