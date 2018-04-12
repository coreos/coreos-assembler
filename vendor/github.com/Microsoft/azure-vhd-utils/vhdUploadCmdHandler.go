package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"runtime"
	"strconv"
	"strings"

	"github.com/Azure/azure-sdk-for-go/storage"
	"github.com/Microsoft/azure-vhd-utils/upload"
	"github.com/Microsoft/azure-vhd-utils/upload/metadata"
	"github.com/Microsoft/azure-vhd-utils/vhdcore/common"
	"github.com/Microsoft/azure-vhd-utils/vhdcore/diskstream"
	"github.com/Microsoft/azure-vhd-utils/vhdcore/validator"
	"gopkg.in/urfave/cli.v1"
)

func vhdUploadCmdHandler() cli.Command {
	return cli.Command{
		Name:  "upload",
		Usage: "Upload a local VHD to Azure storage as page blob",
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:  "localvhdpath",
				Usage: "Path to source VHD in the local machine.",
			},
			cli.StringFlag{
				Name:  "stgaccountname",
				Usage: "Azure storage account name.",
			},
			cli.StringFlag{
				Name:  "stgaccountkey",
				Usage: "Azure storage account key.",
			},
			cli.StringFlag{
				Name:  "containername",
				Usage: "Name of the container holding destination page blob. (Default: vhds)",
			},
			cli.StringFlag{
				Name:  "blobname",
				Usage: "Name of the destination page blob.",
			},
			cli.StringFlag{
				Name:  "parallelism",
				Usage: "Number of concurrent goroutines to be used for upload",
			},
			cli.BoolFlag{
				Name:  "overwrite",
				Usage: "Overwrite the blob if already exists.",
			},
		},
		Action: func(c *cli.Context) error {
			const PageBlobPageSize int64 = 2 * 1024 * 1024

			localVHDPath := c.String("localvhdpath")
			if localVHDPath == "" {
				return errors.New("Missing required argument --localvhdpath")
			}

			stgAccountName := c.String("stgaccountname")
			if stgAccountName == "" {
				return errors.New("Missing required argument --stgaccountname")
			}

			stgAccountKey := c.String("stgaccountkey")
			if stgAccountKey == "" {
				return errors.New("Missing required argument --stgaccountkey")
			}

			containerName := c.String("containername")
			if containerName == "" {
				containerName = "vhds"
				log.Println("Using default container 'vhds'")
			}

			blobName := c.String("blobname")
			if blobName == "" {
				return errors.New("Missing required argument --blobname")
			}

			if !strings.HasSuffix(strings.ToLower(blobName), ".vhd") {
				blobName = blobName + ".vhd"
			}

			parallelism := int(0)
			if c.IsSet("parallelism") {
				p, err := strconv.ParseUint(c.String("parallelism"), 10, 32)
				if err != nil {
					return fmt.Errorf("invalid index value --parallelism: %s", err)
				}
				parallelism = int(p)
			} else {
				parallelism = 8 * runtime.NumCPU()
				log.Printf("Using default parallelism [8*NumCPU] : %d\n", parallelism)
			}

			overwrite := c.IsSet("overwrite")

			ensureVHDSanity(localVHDPath)
			diskStream, err := diskstream.CreateNewDiskStream(localVHDPath)
			if err != nil {
				return err
			}
			defer diskStream.Close()

			storageClient, err := storage.NewBasicClient(stgAccountName, stgAccountKey)
			if err != nil {
				return err
			}
			blobServiceClient := storageClient.GetBlobService()
			if _, err = blobServiceClient.CreateContainerIfNotExists(containerName, storage.ContainerAccessTypePrivate); err != nil {
				return err
			}

			blobExists, err := blobServiceClient.BlobExists(containerName, blobName)
			if err != nil {
				return err
			}

			resume := false
			var blobMetaData *metadata.MetaData
			if blobExists {
				if !overwrite {
					blobMetaData = getBlobMetaData(blobServiceClient, containerName, blobName)
					resume = true
					log.Printf("Blob with name '%s' already exists, checking upload can be resumed\n", blobName)
				}
			}

			localMetaData := getLocalVHDMetaData(localVHDPath)
			var rangesToSkip []*common.IndexRange
			if resume {
				if errs := metadata.CompareMetaData(blobMetaData, localMetaData); len(errs) != 0 {
					printErrorsAndFatal(errs)
				}
				rangesToSkip = getAlreadyUploadedBlobRanges(blobServiceClient, containerName, blobName)
			} else {
				createBlob(blobServiceClient, containerName, blobName, diskStream.GetSize(), localMetaData)
			}

			uploadableRanges, err := upload.LocateUploadableRanges(diskStream, rangesToSkip, PageBlobPageSize)
			if err != nil {
				return err
			}

			uploadableRanges, err = upload.DetectEmptyRanges(diskStream, uploadableRanges)
			if err != nil {
				return err
			}

			cxt := &upload.DiskUploadContext{
				VhdStream:             diskStream,
				UploadableRanges:      uploadableRanges,
				AlreadyProcessedBytes: common.TotalRangeLength(rangesToSkip),
				BlobServiceClient:     blobServiceClient,
				ContainerName:         containerName,
				BlobName:              blobName,
				Parallelism:           parallelism,
				Resume:                resume,
				MD5Hash:               localMetaData.FileMetaData.MD5Hash,
			}

			err = upload.Upload(cxt)
			if err != nil {
				return err
			}

			setBlobMD5Hash(blobServiceClient, containerName, blobName, localMetaData)
			fmt.Println("\nUpload completed")
			return nil
		},
	}
}

// printErrorsAndFatal prints the errors in a slice one by one and then exit
//
func printErrorsAndFatal(errs []error) {
	fmt.Println()
	for _, e := range errs {
		fmt.Println(e)
	}
	log.Fatal("Cannot continue due to above errors.")
}

// ensureVHDSanity ensure is VHD is valid for Azure.
//
func ensureVHDSanity(localVHDPath string) {
	if err := validator.ValidateVhd(localVHDPath); err != nil {
		log.Fatal(err)
	}

	if err := validator.ValidateVhdSize(localVHDPath); err != nil {
		log.Fatal(err)
	}
}

// getBlobMetaData returns the custom metadata associated with a page blob which is set by createBlob method.
// The parameter client is the Azure blob service client, parameter containerName is the name of an existing container
// in which the page blob resides, parameter blobName is name for the page blob
// This method attempt to fetch the metadata only if MD5Hash is not set for the page blob, this method panic if the
// MD5Hash is already set or if the custom metadata is absent.
//
func getBlobMetaData(client storage.BlobStorageClient, containerName, blobName string) *metadata.MetaData {
	md5Hash := getBlobMD5Hash(client, containerName, blobName)
	if md5Hash != "" {
		log.Fatalf("VHD exists in blob storage with name '%s'. If you want to upload again, use the --overwrite option.", blobName)
	}

	blobMetaData, err := metadata.NewMetadataFromBlob(client, containerName, blobName)
	if err != nil {
		log.Fatal(err)
	}

	if blobMetaData == nil {
		log.Fatalf("There is no upload metadata associated with the existing blob '%s', so upload operation cannot be resumed, use --overwrite option.", blobName)
	}
	return blobMetaData
}

// getLocalVHDMetaData returns the metadata of a local VHD
//
func getLocalVHDMetaData(localVHDPath string) *metadata.MetaData {
	localMetaData, err := metadata.NewMetaDataFromLocalVHD(localVHDPath)
	if err != nil {
		log.Fatal(err)
	}
	return localMetaData
}

// createBlob creates a page blob of specific size and sets custom metadata
// The parameter client is the Azure blob service client, parameter containerName is the name of an existing container
// in which the page blob needs to be created, parameter blobName is name for the new page blob, size is the size of
// the new page blob in bytes and parameter vhdMetaData is the custom metadata to be associacted with the page blob
//
func createBlob(client storage.BlobStorageClient, containerName, blobName string, size int64, vhdMetaData *metadata.MetaData) {
	if err := client.PutPageBlob(containerName, blobName, size, nil); err != nil {
		log.Fatal(err)
	}
	m, _ := vhdMetaData.ToMap()
	if err := client.SetBlobMetadata(containerName, blobName, m, make(map[string]string)); err != nil {
		log.Fatal(err)
	}
}

// setBlobMD5Hash sets MD5 hash of the blob in it's properties
//
func setBlobMD5Hash(client storage.BlobStorageClient, containerName, blobName string, vhdMetaData *metadata.MetaData) {
	if vhdMetaData.FileMetaData.MD5Hash != nil {
		blobHeaders := storage.BlobHeaders{
			ContentMD5: base64.StdEncoding.EncodeToString(vhdMetaData.FileMetaData.MD5Hash),
		}
		if err := client.SetBlobProperties(containerName, blobName, blobHeaders); err != nil {
			log.Fatal(err)
		}
	}
}

// getAlreadyUploadedBlobRanges returns the range slice containing ranges of a page blob those are already uploaded.
// The parameter client is the Azure blob service client, parameter containerName is the name of an existing container
// in which the page blob resides, parameter blobName is name for the page blob
//
func getAlreadyUploadedBlobRanges(client storage.BlobStorageClient, containerName, blobName string) []*common.IndexRange {
	existingRanges, err := client.GetPageRanges(containerName, blobName)
	if err != nil {
		log.Fatal(err)
	}
	var rangesToSkip = make([]*common.IndexRange, len(existingRanges.PageList))
	for i, r := range existingRanges.PageList {
		rangesToSkip[i] = common.NewIndexRange(r.Start, r.End)
	}
	return rangesToSkip
}

// getBlobMD5Hash returns the MD5Hash associated with a blob
// The parameter client is the Azure blob service client, parameter containerName is the name of an existing container
// in which the page blob resides, parameter blobName is name for the page blob
//
func getBlobMD5Hash(client storage.BlobStorageClient, containerName, blobName string) string {
	properties, err := client.GetBlobProperties(containerName, blobName)
	if err != nil {
		log.Fatal(err)
	}
	return properties.ContentMD5
}
