// Azure VHD Utilities for Go
// Copyright (c) Microsoft Corporation
//
// All rights reserved.
//
// MIT License
//
// Permission is hereby granted, free of charge, to any person obtaining a copy of
// this software and associated documentation files (the "Software"), to deal in
// the Software without restriction, including without limitation the rights to
// use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies
// of the Software, and to permit persons to whom the Software is furnished to do
// so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED *AS IS*, WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.
//
// derived from https://github.com/Microsoft/azure-vhd-utils/blob/8fcb4e03cb4c0f928aa835c21708182dbb23fc83/vhdUploadCmdHandler.go

package azure

import (
	"fmt"
	"io"

	"github.com/Azure/azure-sdk-for-go/storage"
	"github.com/Microsoft/azure-vhd-utils/upload"
	"github.com/Microsoft/azure-vhd-utils/upload/metadata"
	"github.com/Microsoft/azure-vhd-utils/vhdcore/common"
	"github.com/Microsoft/azure-vhd-utils/vhdcore/diskstream"
	"github.com/coreos/pkg/multierror"
)

const pageBlobPageSize int64 = 2 * 1024 * 1024

type BlobExistsError string

func (be BlobExistsError) Error() string {
	return fmt.Sprintf("blob %q already exists", string(be))
}

func (a *API) BlobExists(storageaccount, storagekey, container, blob string) (bool, error) {
	sc, err := storage.NewClient(storageaccount, storagekey, a.opts.StorageEndpointSuffix, storage.DefaultAPIVersion, true)
	if err != nil {
		return false, err
	}

	bsc := sc.GetBlobService()

	return bsc.BlobExists(container, blob)
}

func (a *API) GetBlob(storageaccount, storagekey, container, name string) (io.ReadCloser, error) {
	sc, err := storage.NewClient(storageaccount, storagekey, a.opts.StorageEndpointSuffix, storage.DefaultAPIVersion, true)
	if err != nil {
		return nil, err
	}

	bsc := sc.GetBlobService()
	if _, err = bsc.CreateContainerIfNotExists(container, storage.ContainerAccessTypePrivate); err != nil {
		return nil, err
	}

	return bsc.GetBlob(container, name)
}

// DeleteBlob deletes the given blob specified by the given storage account,
// container, and blob name.
func (a *API) DeleteBlob(storageaccount, storagekey, container, blob string) error {
	sc, err := storage.NewClient(storageaccount, storagekey, a.opts.StorageEndpointSuffix, storage.DefaultAPIVersion, true)
	if err != nil {
		return err
	}

	bsc := sc.GetBlobService()
	if _, err = bsc.CreateContainerIfNotExists(container, storage.ContainerAccessTypePrivate); err != nil {
		return err
	}

	err = bsc.DeleteBlob(container, blob, nil)
	if err != nil {
		return err
	}

	return nil
}

// UploadBlob uploads vhd to the given storage account, container, and blob name.
//
// It returns BlobExistsError if the blob exists and overwrite is not true.
func (a *API) UploadBlob(storageaccount, storagekey, vhd, container, blob string, overwrite bool) error {
	ds, err := diskstream.CreateNewDiskStream(vhd)
	if err != nil {
		return err
	}
	defer ds.Close()

	sc, err := storage.NewClient(storageaccount, storagekey, a.opts.StorageEndpointSuffix, storage.DefaultAPIVersion, true)
	if err != nil {
		return err
	}

	bsc := sc.GetBlobService()
	if _, err = bsc.CreateContainerIfNotExists(container, storage.ContainerAccessTypePrivate); err != nil {
		return err
	}

	blobExists, err := bsc.BlobExists(container, blob)
	if err != nil {
		return err
	}

	resume := false
	var blobMetaData *metadata.MetaData
	if blobExists {
		if !overwrite {
			bm, err := getBlobMetaData(bsc, container, blob)
			if err != nil {
				return err
			}
			blobMetaData = bm
			resume = true
			plog.Printf("Blob with name '%s' already exists, checking if upload can be resumed", blob)
		}
	}

	localMetaData, err := getLocalVHDMetaData(vhd)
	if err != nil {
		return err
	}
	var rangesToSkip []*common.IndexRange
	if resume {
		if errs := metadata.CompareMetaData(blobMetaData, localMetaData); len(errs) != 0 {
			return multierror.Error(errs)
		}
		ranges, err := getAlreadyUploadedBlobRanges(bsc, container, blob)
		if err != nil {
			return err
		}
		rangesToSkip = ranges
	} else {
		if err := createBlob(bsc, container, blob, ds.GetSize(), localMetaData); err != nil {
			return err
		}
	}

	uploadableRanges, err := upload.LocateUploadableRanges(ds, rangesToSkip, pageBlobPageSize)
	if err != nil {
		return err
	}

	uploadableRanges, err = upload.DetectEmptyRanges(ds, uploadableRanges)
	if err != nil {
		return err
	}

	cxt := &upload.DiskUploadContext{
		VhdStream:             ds,
		UploadableRanges:      uploadableRanges,
		AlreadyProcessedBytes: common.TotalRangeLength(rangesToSkip),
		BlobServiceClient:     bsc,
		ContainerName:         container,
		BlobName:              blob,
		Parallelism:           8,
		Resume:                resume,
		MD5Hash:               localMetaData.FileMetaData.MD5Hash,
	}

	return upload.Upload(cxt)
}

// getBlobMetaData returns the custom metadata associated with a page blob which is set by createBlob method.
// The parameter client is the Azure blob service client, parameter containerName is the name of an existing container
// in which the page blob resides, parameter blobName is name for the page blob
// This method attempt to fetch the metadata only if MD5Hash is not set for the page blob, this method panic if the
// MD5Hash is already set or if the custom metadata is absent.
//
func getBlobMetaData(client storage.BlobStorageClient, containerName, blobName string) (*metadata.MetaData, error) {
	md5Hash, err := getBlobMD5Hash(client, containerName, blobName)
	if md5Hash != "" {
		return nil, BlobExistsError(blobName)
	}
	if err != nil {
		return nil, err
	}

	blobMetaData, err := metadata.NewMetadataFromBlob(client, containerName, blobName)
	if err != nil {
		return nil, err
	}

	if blobMetaData == nil {
		return nil, fmt.Errorf("There is no upload metadata associated with the existing blob '%s', so upload operation cannot be resumed, use --overwrite option.", blobName)
	}

	return blobMetaData, nil
}

// getLocalVHDMetaData returns the metadata of a local VHD
//
func getLocalVHDMetaData(localVHDPath string) (*metadata.MetaData, error) {
	localMetaData, err := metadata.NewMetaDataFromLocalVHD(localVHDPath)
	if err != nil {
		return nil, err
	}
	return localMetaData, nil
}

// createBlob creates a page blob of specific size and sets custom metadata
// The parameter client is the Azure blob service client, parameter containerName is the name of an existing container
// in which the page blob needs to be created, parameter blobName is name for the new page blob, size is the size of
// the new page blob in bytes and parameter vhdMetaData is the custom metadata to be associacted with the page blob
//
func createBlob(client storage.BlobStorageClient, containerName, blobName string, size int64, vhdMetaData *metadata.MetaData) error {
	if err := client.PutPageBlob(containerName, blobName, size, nil); err != nil {
		return err
	}
	m, _ := vhdMetaData.ToMap()
	if err := client.SetBlobMetadata(containerName, blobName, m, make(map[string]string)); err != nil {
		return err
	}

	return nil
}

// getAlreadyUploadedBlobRanges returns the range slice containing ranges of a page blob those are already uploaded.
// The parameter client is the Azure blob service client, parameter containerName is the name of an existing container
// in which the page blob resides, parameter blobName is name for the page blob
//
func getAlreadyUploadedBlobRanges(client storage.BlobStorageClient, containerName, blobName string) ([]*common.IndexRange, error) {
	existingRanges, err := client.GetPageRanges(containerName, blobName)
	if err != nil {
		return nil, err
	}
	var rangesToSkip = make([]*common.IndexRange, len(existingRanges.PageList))
	for i, r := range existingRanges.PageList {
		rangesToSkip[i] = common.NewIndexRange(r.Start, r.End)
	}
	return rangesToSkip, nil
}

// getBlobMD5Hash returns the MD5Hash associated with a blob
// The parameter client is the Azure blob service client, parameter containerName is the name of an existing container
// in which the page blob resides, parameter blobName is name for the page blob
//
func getBlobMD5Hash(client storage.BlobStorageClient, containerName, blobName string) (string, error) {
	properties, err := client.GetBlobProperties(containerName, blobName)
	if err != nil {
		return "", err
	}
	return properties.ContentMD5, nil
}
