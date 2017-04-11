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
	"io"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

const (
	// The SDK documentation claims the error code should be `NoSuchKey`, but in
	// practice that's the error for Get and NotFound is the error for Head.
	// https://github.com/aws/aws-sdk-go/blob/b84b5a456f5f281454e9fbe89b38e34d617f4a51/service/s3/api.go#L2618-L2620
	// is just wrong.
	documentedNotFoundErr = "NoSuchKey"
	actualNotFoundErr     = "NotFound"

	alreadyExistsErr = "BucketAlreadyOwnedByYou"
)

func s3IsNotFound(err error) bool {
	if awserr, ok := err.(awserr.Error); ok {
		return awserr.Code() == documentedNotFoundErr || awserr.Code() == actualNotFoundErr
	}
	return false
}

// UploadObject uploads an object to S3
func (a *API) UploadObject(r io.Reader, bucket, path string, force bool) error {
	s3uploader := s3manager.NewUploaderWithClient(a.s3)

	if !force {
		_, err := a.s3.HeadObject(&s3.HeadObjectInput{
			Bucket: &bucket,
			Key:    &path,
		})
		if err != nil {
			if !s3IsNotFound(err) {
				return fmt.Errorf("unable to head object %v/%v: %v", bucket, path, err)
			}
		} else {
			plog.Infof("skipping upload since force was not set: s3://%v/%v", bucket, path)
			return nil
		}
	}

	_, err := s3uploader.Upload(&s3manager.UploadInput{
		Body:   r,
		Bucket: aws.String(bucket),
		Key:    aws.String(path),
	})
	if err != nil {
		return fmt.Errorf("error uploading s3://%v/%v: %v", bucket, path, err)
	}
	return err
}

func (a *API) DeleteObject(bucket, path string) error {
	plog.Infof("deleting s3://%v/%v", bucket, path)
	_, err := a.s3.DeleteObject(&s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(path),
	})
	if err != nil {
		return fmt.Errorf("error deleting s3://%v/%v: %v", bucket, path, err)
	}
	return err
}

func (a *API) InitializeBucket(bucket string) error {
	_, err := a.s3.CreateBucket(&s3.CreateBucketInput{
		Bucket: &bucket,
	})
	if err != nil {
		if awserr, ok := err.(awserr.Error); ok {
			if awserr.Code() == alreadyExistsErr {
				return nil
			}
		}
	}
	return err
}
