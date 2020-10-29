/*
 * MinIO Cloud Storage, (C) 2016-2020 MinIO, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package cmd

import (
	"context"
	"errors"

	"github.com/minio/minio-go/v7/pkg/s3utils"
	"github.com/minio/minio/cmd/logger"
	"github.com/minio/minio/pkg/sync/errgroup"
)

// list all errors that can be ignore in a bucket operation.
var bucketOpIgnoredErrs = append(baseIgnoredErrs, errDiskAccessDenied, errUnformattedDisk)

// list all errors that can be ignored in a bucket metadata operation.
var bucketMetadataOpIgnoredErrs = append(bucketOpIgnoredErrs, errVolumeNotFound)

/// Bucket operations

// MakeBucket - make a bucket.
func (er erasureObjects) MakeBucketWithLocation(ctx context.Context, bucket string, opts BucketOptions) error {
	// Verify if bucket is valid.
	if err := s3utils.CheckValidBucketNameStrict(bucket); err != nil {
		return BucketNameInvalid{Bucket: bucket}
	}

	storageDisks := er.getDisks()

	g := errgroup.WithNErrs(len(storageDisks))

	// Make a volume entry on all underlying storage disks.
	for index := range storageDisks {
		index := index
		g.Go(func() error {
			if storageDisks[index] != nil {
				if err := storageDisks[index].MakeVol(ctx, bucket); err != nil {
					if !errors.Is(err, errVolumeExists) {
						logger.LogIf(ctx, err)
					}
					return err
				}
				return nil
			}
			return errDiskNotFound
		}, index)
	}

	writeQuorum := getWriteQuorum(len(storageDisks))
	err := reduceWriteQuorumErrs(ctx, g.Wait(), bucketOpIgnoredErrs, writeQuorum)
	return toObjectErr(err, bucket)
}

func undoDeleteBucket(storageDisks []StorageAPI, bucket string) {
	g := errgroup.WithNErrs(len(storageDisks))
	// Undo previous make bucket entry on all underlying storage disks.
	for index := range storageDisks {
		if storageDisks[index] == nil {
			continue
		}
		index := index
		g.Go(func() error {
			_ = storageDisks[index].MakeVol(context.Background(), bucket)
			return nil
		}, index)
	}

	// Wait for all make vol to finish.
	g.Wait()
}

// getBucketInfo - returns the BucketInfo from one of the load balanced disks.
func (er erasureObjects) getBucketInfo(ctx context.Context, bucketName string) (bucketInfo BucketInfo, err error) {
	storageDisks := er.getDisks()

	g := errgroup.WithNErrs(len(storageDisks))
	var bucketsInfo = make([]BucketInfo, len(storageDisks))
	// Undo previous make bucket entry on all underlying storage disks.
	for index := range storageDisks {
		index := index
		g.Go(func() error {
			if storageDisks[index] == nil {
				return errDiskNotFound
			}
			volInfo, err := storageDisks[index].StatVol(ctx, bucketName)
			if err != nil {
				return err
			}
			bucketsInfo[index] = BucketInfo(volInfo)
			return nil
		}, index)
	}

	errs := g.Wait()

	for i, err := range errs {
		if err == nil {
			return bucketsInfo[i], nil
		}
	}

	// If all our errors were ignored, then we try to
	// reduce to one error based on read quorum.
	// `nil` is deliberately passed for ignoredErrs
	// because these errors were already ignored.
	readQuorum := getReadQuorum(len(storageDisks))
	return BucketInfo{}, reduceReadQuorumErrs(ctx, errs, nil, readQuorum)
}

// GetBucketInfo - returns BucketInfo for a bucket.
func (er erasureObjects) GetBucketInfo(ctx context.Context, bucket string) (bi BucketInfo, e error) {
	bucketInfo, err := er.getBucketInfo(ctx, bucket)
	if err != nil {
		return bi, toObjectErr(err, bucket)
	}
	return bucketInfo, nil
}

// Dangling buckets should be handled appropriately, in this following situation
// we actually have quorum error to be `nil` but we have some disks where
// the bucket delete returned `errVolumeNotEmpty` but this is not correct
// can only happen if there are dangling objects in a bucket. Under such
// a situation we simply attempt a full delete of the bucket including
// the dangling objects. All of this happens under a lock and there
// is no way a user can create buckets and sneak in objects into namespace,
// so it is safer to do.
func deleteDanglingBucket(ctx context.Context, storageDisks []StorageAPI, dErrs []error, bucket string) {
	for index, err := range dErrs {
		if err == errVolumeNotEmpty {
			// Attempt to delete bucket again.
			if derr := storageDisks[index].DeleteVol(ctx, bucket, false); derr == errVolumeNotEmpty {
				_ = cleanupDir(ctx, storageDisks[index], bucket, "")

				_ = storageDisks[index].DeleteVol(ctx, bucket, false)

				// Cleanup all the previously incomplete multiparts.
				_ = cleanupDir(ctx, storageDisks[index], minioMetaMultipartBucket, bucket)
			}
		}
	}
}

// DeleteBucket - deletes a bucket.
func (er erasureObjects) DeleteBucket(ctx context.Context, bucket string, forceDelete bool) error {
	// Collect if all disks report volume not found.
	defer ObjectPathUpdated(bucket + slashSeparator)
	storageDisks := er.getDisks()

	g := errgroup.WithNErrs(len(storageDisks))

	for index := range storageDisks {
		index := index
		g.Go(func() error {
			if storageDisks[index] != nil {
				if err := storageDisks[index].DeleteVol(ctx, bucket, forceDelete); err != nil {
					return err
				}
				err := cleanupDir(ctx, storageDisks[index], minioMetaMultipartBucket, bucket)
				if err != nil && err != errVolumeNotFound {
					return err
				}
				return nil
			}
			return errDiskNotFound
		}, index)
	}

	// Wait for all the delete vols to finish.
	dErrs := g.Wait()

	if forceDelete {
		for _, err := range dErrs {
			if err != nil {
				undoDeleteBucket(storageDisks, bucket)
				return toObjectErr(err, bucket)
			}
		}

		return nil
	}

	writeQuorum := getWriteQuorum(len(storageDisks))
	err := reduceWriteQuorumErrs(ctx, dErrs, bucketOpIgnoredErrs, writeQuorum)
	if err == errErasureWriteQuorum {
		undoDeleteBucket(storageDisks, bucket)
	}
	if err != nil {
		return toObjectErr(err, bucket)
	}

	// If we reduce quorum to nil, means we have deleted buckets properly
	// on some servers in quorum, we should look for volumeNotEmpty errors
	// and delete those buckets as well.
	//
	// let this call succeed, even if client cancels the context
	// this is to ensure that we don't leave any stale content
	deleteDanglingBucket(context.Background(), storageDisks, dErrs, bucket)

	return nil
}

// IsNotificationSupported returns whether bucket notification is applicable for this layer.
func (er erasureObjects) IsNotificationSupported() bool {
	return true
}

// IsListenSupported returns whether listen bucket notification is applicable for this layer.
func (er erasureObjects) IsListenSupported() bool {
	return true
}

// IsEncryptionSupported returns whether server side encryption is implemented for this layer.
func (er erasureObjects) IsEncryptionSupported() bool {
	return true
}

// IsCompressionSupported returns whether compression is applicable for this layer.
func (er erasureObjects) IsCompressionSupported() bool {
	return true
}

// IsTaggingSupported indicates whether erasureObjects implements tagging support.
func (er erasureObjects) IsTaggingSupported() bool {
	return true
}
