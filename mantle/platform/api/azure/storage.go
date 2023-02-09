// Copyright 2023 Red Hat
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

package azure

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/streaming"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/storage/armstorage"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/pageblob"

	"github.com/frostschutz/go-fibmap"
)

func (a *API) GetStorageServiceKeys(account, resourceGroup string) (armstorage.AccountListKeysResult, error) {
	resp, err := a.accClient.ListKeys(context.Background(), resourceGroup, account, &armstorage.AccountsClientListKeysOptions{Expand: nil})
	if err != nil {
		return armstorage.AccountListKeysResult{}, err
	}
	return resp.AccountListKeysResult, nil
}

func (a *API) CreateStorageAccount(resourceGroup string) (string, error) {
	// Only lower-case letters & numbers allowed in storage account names
	name := strings.Replace(randomName("kolasa"), "-", "", -1)
	parameters := armstorage.AccountCreateParameters{
		SKU: &armstorage.SKU{
			Name: to.Ptr(armstorage.SKUNameStandardLRS),
		},
		Kind:     to.Ptr(armstorage.KindStorage),
		Location: to.Ptr(a.opts.Location),
	}
	ctx := context.Background()
	poller, err := a.accClient.BeginCreate(ctx, resourceGroup, name, parameters, nil)
	if err != nil {
		return "", fmt.Errorf("creating storage account: %v", err)
	}
	_, err = poller.PollUntilDone(ctx, nil)
	return name, err
}

func (a *API) GetBlockBlob(storageaccount, key, container, name string) (io.ReadCloser, error) {
	client, err := getBlockBlobClient(storageaccount, key)
	if err != nil {
		return nil, err
	}

	resp, err := client.DownloadStream(context.Background(), container, name, nil)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func (a *API) PageBlobExists(storageaccount, key, container, blobname string) (bool, error) {
	client, err := getPageBlobClient(storageaccount, key, container, blobname)
	if err != nil {
		return false, err
	}
	// Use GetProperties here since there isn't a better way to detect
	// if a page blob exists.
	_, err = client.GetProperties(context.Background(), nil)
	if err != nil {
		if bloberror.HasCode(err, bloberror.BlobNotFound) {
			return false, nil
		} else {
			return false, err
		}
	}
	return true, nil
}

func (a *API) UploadPageBlob(storageaccount, key, file, container, blobname string) error {
	client, err := getPageBlobClient(storageaccount, key, container, blobname)
	if err != nil {
		return err
	}
	// Open the file and get the size in bytes
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	size := fi.Size()

	// Create the page blob
	ctx := context.Background()
	_, err = client.Create(ctx, size, nil)
	if err != nil {
		return err
	}

	// Find the data (non-zero) ranges in the file and then chunk up
	// those data ranges so they are in 4MiB segments which is the
	// maxiumum that can be uploaded in one call to UploadPages().
	dataRanges := fibmap.NewFibmapFile(f).SeekDataHole()
	var chunkedDataRanges []int64
	dataSize, fourMB := int64(0), int64(4*1024*1024)
	for i := 0; i < len(dataRanges); i += 2 {
		offset, count := dataRanges[i], dataRanges[i+1]
		end := offset + count
		dataSize += count
		for offset < end {
			chunk := fourMB
			if (end - offset) < fourMB {
				chunk = end - offset
			}
			chunkedDataRanges = append(chunkedDataRanges, offset, chunk)
			offset += chunk
		}
	}
	fmt.Printf("\nEffective upload size: %d MiB (from %d MiB originally)\n", dataSize/1024/1024, size/1024/1024)

	// Upload the data using UploadPages() and show progress. Use a SectionReader
	// to give the UploadPages a specific window of data to operate on. Use
	// streaming.NopCloser to allow passing in a Reader with no Close() implementation.
	uploaded := int64(0)
	for i := 0; i < len(chunkedDataRanges); i += 2 {
		offset, count := chunkedDataRanges[i], chunkedDataRanges[i+1]
		sr := io.NewSectionReader(f, offset, count)
		_, err = client.UploadPages(ctx, streaming.NopCloser(sr), blob.HTTPRange{
			Offset: offset,
			Count:  count,
		}, nil)
		if err != nil {
			return err
		}
		uploaded += count
		fmt.Printf("\033[2K\rProgress: %v%%", uploaded*100/dataSize)
	}
	return nil
}

func (a *API) DeletePageBlob(storageaccount, key, container, blobname string) error {
	client, err := getPageBlobClient(storageaccount, key, container, blobname)
	if err != nil {
		return err
	}
	_, err = client.Delete(context.Background(), nil)
	return err
}

func (a *API) DeleteBlockBlob(storageaccount, key, container, blob string) error {
	client, err := getBlockBlobClient(storageaccount, key)
	if err != nil {
		return err
	}
	_, err = client.DeleteBlob(context.Background(), container, blob, nil)
	return err
}

func getBlockBlobClient(storageaccount, key string) (*azblob.Client, error) {
	serviceURL := fmt.Sprintf("https://%s.blob.core.windows.net/", storageaccount)
	cred, err := azblob.NewSharedKeyCredential(storageaccount, key)
	if err != nil {
		return nil, err
	}
	return azblob.NewClientWithSharedKeyCredential(serviceURL, cred, nil)
}

func getPageBlobClient(storageaccount, key, container, blobname string) (*pageblob.Client, error) {
	pageBlobURL := fmt.Sprintf("https://%s.blob.core.windows.net/%s/%s", storageaccount, container, blobname)
	cred, err := azblob.NewSharedKeyCredential(storageaccount, key)
	if err != nil {
		return nil, err
	}
	return pageblob.NewClientWithSharedKeyCredential(pageBlobURL, cred, nil)
}
