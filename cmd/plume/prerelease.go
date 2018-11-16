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
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/management/storageservice"
	"github.com/Microsoft/azure-vhd-utils/vhdcore/validator"
	"github.com/spf13/cobra"
	"golang.org/x/net/context"
	gs "google.golang.org/api/storage/v1"

	"github.com/coreos/mantle/auth"
	"github.com/coreos/mantle/platform/api/aws"
	"github.com/coreos/mantle/platform/api/azure"
	"github.com/coreos/mantle/sdk"
	"github.com/coreos/mantle/storage"
	"github.com/coreos/mantle/util"
)

var (
	cmdPreRelease = &cobra.Command{
		Use:   "pre-release [options]",
		Short: "Run pre-release steps for CoreOS",
		Long:  "Runs pre-release steps for CoreOS, such as image uploading and OS image creation, and replication across regions.",
		RunE:  runPreRelease,
	}

	platforms = map[string]platform{
		"aws": platform{
			displayName: "AWS",
			handler:     awsPreRelease,
		},
		"azure": platform{
			displayName: "Azure",
			handler:     azurePreRelease,
		},
	}
	platformList []string

	selectedPlatforms  []string
	azureProfile       string
	awsCredentialsFile string
	verifyKeyFile      string
	imageInfoFile      string
)

type platform struct {
	displayName string
	handler     func(context.Context, *http.Client, *storage.Bucket, *channelSpec, *imageInfo) error
}

type imageInfo struct {
	AWS   *amiList        `json:"aws,omitempty"`
	Azure *azureImageInfo `json:"azure,omitempty"`
}

func init() {
	for k, _ := range platforms {
		platformList = append(platformList, k)
	}
	sort.Sort(sort.StringSlice(platformList))

	cmdPreRelease.Flags().StringSliceVar(&selectedPlatforms, "platform", platformList, "platform to pre-release")
	cmdPreRelease.Flags().StringVar(&azureProfile, "azure-profile", "", "Azure Profile json file")
	cmdPreRelease.Flags().StringVar(&awsCredentialsFile, "aws-credentials", "", "AWS credentials file")
	cmdPreRelease.Flags().StringVar(&verifyKeyFile,
		"verify-key", "", "path to ASCII-armored PGP public key to be used in verifying download signatures.  Defaults to CoreOS Buildbot (0412 7D0B FABE C887 1FFB  2CCE 50E0 8855 93D2 DCB4)")
	cmdPreRelease.Flags().StringVar(&imageInfoFile, "write-image-list", "", "optional output file describing uploaded images")

	AddSpecFlags(cmdPreRelease.Flags())
	root.AddCommand(cmdPreRelease)
}

func runPreRelease(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return errors.New("no args accepted")
	}
	for _, platformName := range selectedPlatforms {
		if _, ok := platforms[platformName]; !ok {
			return fmt.Errorf("Unknown platform %q", platformName)
		}
	}

	spec := ChannelSpec()
	ctx := context.Background()
	client, err := getGoogleClient()
	if err != nil {
		plog.Fatal(err)
	}

	src, err := storage.NewBucket(client, spec.SourceURL())
	if err != nil {
		plog.Fatal(err)
	}

	if err := src.Fetch(ctx); err != nil {
		plog.Fatal(err)
	}

	// Sanity check!
	if vertxt := src.Object(src.Prefix() + "version.txt"); vertxt == nil {
		verurl := src.URL().String() + "version.txt"
		plog.Fatalf("File not found: %s", verurl)
	}

	var imageInfo imageInfo
	for _, platformName := range platformList {
		run := false
		for _, v := range selectedPlatforms {
			if v == platformName {
				run = true
				break
			}
		}
		if !run {
			continue
		}

		platform := platforms[platformName]
		plog.Printf("Running %v pre-release...", platform.displayName)
		if err := platform.handler(ctx, client, src, &spec, &imageInfo); err != nil {
			plog.Fatal(err)
		}
	}

	if imageInfoFile != "" {
		f, err := os.OpenFile(imageInfoFile, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0666)
		if err != nil {
			plog.Fatal(err)
		}
		defer f.Close()

		encoder := json.NewEncoder(f)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(imageInfo); err != nil {
			plog.Fatalf("couldn't encode image list: %v", err)
		}
	}

	plog.Printf("Pre-release complete, run `plume release` to finish.")

	return nil
}

// getImageFile downloads a bzipped CoreOS image, verifies its signature,
// decompresses it, and returns the decompressed path.
func getImageFile(client *http.Client, src *storage.Bucket, fileName string) (string, error) {
	cacheDir := filepath.Join(sdk.RepoCache(), "images", specChannel, specBoard, specVersion)
	bzipPath := filepath.Join(cacheDir, fileName)
	imagePath := strings.TrimSuffix(bzipPath, filepath.Ext(bzipPath))

	if _, err := os.Stat(imagePath); err == nil {
		plog.Printf("Reusing existing image %q", imagePath)
		return imagePath, nil
	}

	bzipUri, err := url.Parse(fileName)
	if err != nil {
		return "", err
	}

	bzipUri = src.URL().ResolveReference(bzipUri)

	plog.Printf("Downloading image %q to %q", bzipUri, bzipPath)

	if err := sdk.UpdateSignedFile(bzipPath, bzipUri.String(), client, verifyKeyFile); err != nil {
		return "", err
	}

	// decompress it
	plog.Printf("Decompressing %q...", bzipPath)
	if err := util.Bunzip2File(imagePath, bzipPath); err != nil {
		return "", err
	}
	return imagePath, nil
}

func uploadAzureBlob(spec *channelSpec, api *azure.API, storageKey storageservice.GetStorageServiceKeysResponse, vhdfile, container, blobName string) error {
	blobExists, err := api.BlobExists(spec.Azure.StorageAccount, storageKey.PrimaryKey, container, blobName)
	if err != nil {
		return fmt.Errorf("failed to check if file %q in account %q container %q exists: %v", vhdfile, spec.Azure.StorageAccount, container, err)
	}

	if blobExists {
		return nil
	}

	if err := api.UploadBlob(spec.Azure.StorageAccount, storageKey.PrimaryKey, vhdfile, container, blobName, false); err != nil {
		if _, ok := err.(azure.BlobExistsError); !ok {
			return fmt.Errorf("uploading file %q to account %q container %q failed: %v", vhdfile, spec.Azure.StorageAccount, container, err)
		}
	}
	return nil
}

func createAzureImage(spec *channelSpec, api *azure.API, blobName, imageName string) error {
	imageexists, err := api.OSImageExists(imageName)
	if err != nil {
		return fmt.Errorf("failed to check if image %q exists: %T %v", imageName, err, err)
	}

	if imageexists {
		plog.Printf("OS Image %q exists, using it", imageName)
		return nil
	}

	plog.Printf("Creating OS image with name %q", imageName)

	bloburl := api.UrlOfBlob(spec.Azure.StorageAccount, spec.Azure.Container, blobName).String()

	// a la https://github.com/coreos/scripts/blob/998c7e093922298637e7c7e82e25cee7d336144d/oem/azure/set-image-metadata.sh
	md := &azure.OSImage{
		Label:             spec.Azure.Label,
		Name:              imageName,
		OS:                "Linux",
		Description:       spec.Azure.Description,
		MediaLink:         bloburl,
		ImageFamily:       spec.Azure.Label,
		PublishedDate:     time.Now().UTC().Format("2006-01-02"),
		RecommendedVMSize: spec.Azure.RecommendedVMSize,
		IconURI:           spec.Azure.IconURI,
		SmallIconURI:      spec.Azure.SmallIconURI,
	}

	return api.AddOSImage(md)
}

func replicateAzureImage(spec *channelSpec, api *azure.API, imageName string) error {
	plog.Printf("Fetching Azure Locations...")
	locations, err := api.Locations()
	if err != nil {
		return err
	}

	plog.Printf("Replicating image to locations: %s", strings.Join(locations, ", "))

	channelTitle := strings.Title(specChannel)

	if err := api.ReplicateImage(imageName, spec.Azure.Offer, channelTitle, specVersion, locations...); err != nil {
		return fmt.Errorf("image replication failed: %v", err)
	}

	return nil
}

type azureImageInfo struct {
	ImageName string `json:"image"`
}

// azurePreRelease runs everything necessary to prepare a CoreOS release for Azure.
//
// This includes uploading the vhd image to Azure storage, creating an OS image from it,
// and replicating that OS image.
func azurePreRelease(ctx context.Context, client *http.Client, src *storage.Bucket, spec *channelSpec, imageInfo *imageInfo) error {
	if spec.Azure.StorageAccount == "" {
		plog.Notice("Azure image creation disabled.")
		return nil
	}

	prof, err := auth.ReadAzureProfile(azureProfile)
	if err != nil {
		return fmt.Errorf("failed reading Azure profile: %v", err)
	}

	// download azure vhd image and unzip it
	vhdfile, err := getImageFile(client, src, spec.Azure.Image)
	if err != nil {
		return err
	}

	// sanity check - validate VHD file
	plog.Printf("Validating VHD file %q", vhdfile)
	if err := validator.ValidateVhd(vhdfile); err != nil {
		return err
	}
	if err := validator.ValidateVhdSize(vhdfile); err != nil {
		return err
	}

	blobName := fmt.Sprintf("container-linux-%s-%s.vhd", specVersion, specChannel)
	// channel name should be caps for azure image
	imageName := fmt.Sprintf("%s-%s-%s", spec.Azure.Offer, strings.Title(specChannel), specVersion)

	for _, environment := range spec.Azure.Environments {
		opt := prof.SubscriptionOptions(environment.SubscriptionName)
		if opt == nil {
			return fmt.Errorf("couldn't find subscription %q", environment.SubscriptionName)
		}

		// construct azure api client
		plog.Printf("Creating Azure API from subscription %q endpoint %q", opt.SubscriptionID, opt.ManagementURL)
		api, err := azure.New(opt)
		if err != nil {
			return fmt.Errorf("failed to create Azure API: %v", err)
		}

		plog.Printf("Fetching Azure storage credentials")

		storageKey, err := api.GetStorageServiceKeys(spec.Azure.StorageAccount)
		if err != nil {
			return err
		}

		// upload blob, do not overwrite
		plog.Printf("Uploading %q to Azure Storage...", vhdfile)

		containers := append([]string{spec.Azure.Container}, environment.AdditionalContainers...)
		for _, container := range containers {
			err := uploadAzureBlob(spec, api, storageKey, vhdfile, container, blobName)
			if err != nil {
				return err
			}
		}

		// create image
		if err := createAzureImage(spec, api, blobName, imageName); err != nil {
			// if it is a conflict, it already exists!
			if !azure.IsConflictError(err) {
				return err
			}

			plog.Printf("Azure image %q already exists", imageName)
		}

		// replicate it
		if err := replicateAzureImage(spec, api, imageName); err != nil {
			return err
		}
	}

	imageInfo.Azure = &azureImageInfo{
		ImageName: imageName,
	}
	return nil
}

func awsUploadToPartition(spec *channelSpec, part *awsPartitionSpec, imageName, imageDescription, imagePath string) (map[string]string, map[string]string, error) {
	plog.Printf("Connecting to %v...", part.Name)
	api, err := aws.New(&aws.Options{
		CredentialsFile: awsCredentialsFile,
		Profile:         part.Profile,
		Region:          part.BucketRegion,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("creating client for %v: %v", part.Name, err)
	}

	f, err := os.Open(imagePath)
	if err != nil {
		return nil, nil, fmt.Errorf("Could not open image file %v: %v", imagePath, err)
	}
	defer f.Close()

	s3ObjectPath := fmt.Sprintf("%s/%s/%s", specBoard, specVersion, strings.TrimSuffix(spec.AWS.Image, filepath.Ext(spec.AWS.Image)))
	s3ObjectURL := fmt.Sprintf("s3://%s/%s", part.Bucket, s3ObjectPath)

	snapshot, err := api.FindSnapshot(imageName)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to check for snapshot: %v", err)
	}

	if snapshot == nil {
		plog.Printf("Creating S3 object %v...", s3ObjectURL)
		err = api.UploadObject(f, part.Bucket, s3ObjectPath, false)
		if err != nil {
			return nil, nil, fmt.Errorf("Error uploading: %v", err)
		}

		plog.Printf("Creating EBS snapshot...")
		snapshot, err = api.CreateSnapshot(imageName, s3ObjectURL, aws.EC2ImageFormatVmdk)
		if err != nil {
			return nil, nil, fmt.Errorf("unable to create snapshot: %v", err)
		}
	}

	// delete unconditionally to avoid leaks after a restart
	plog.Printf("Deleting S3 object %v...", s3ObjectURL)
	err = api.DeleteObject(part.Bucket, s3ObjectPath)
	if err != nil {
		return nil, nil, fmt.Errorf("Error deleting S3 object: %v", err)
	}

	plog.Printf("Creating AMIs from %v...", snapshot.SnapshotID)

	hvmImageID, err := api.CreateHVMImage(snapshot.SnapshotID, imageName+"-hvm", 0, imageDescription+" (HVM)")
	if err != nil {
		return nil, nil, fmt.Errorf("unable to create HVM image: %v", err)
	}

	pvImageID, err := api.CreatePVImage(snapshot.SnapshotID, imageName, 0, imageDescription+" (PV)")
	if err != nil {
		return nil, nil, fmt.Errorf("unable to create PV image: %v", err)
	}

	err = api.CreateTags([]string{snapshot.SnapshotID, hvmImageID, pvImageID}, map[string]string{
		"Channel": specChannel,
		"Version": specVersion,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("couldn't tag images: %v", err)
	}

	postprocess := func(imageID string, pv bool) (map[string]string, error) {
		if len(part.LaunchPermissions) > 0 {
			if err := api.GrantLaunchPermission(imageID, part.LaunchPermissions); err != nil {
				return nil, err
			}
		}

		destRegions := make([]string, 0, len(part.Regions))
		foundBucketRegion := false
		for _, region := range part.Regions {
			if region != part.BucketRegion {
				if pv && !aws.RegionSupportsPV(region) {
					plog.Debugf("%v doesn't support PV AMIs; skipping", region)
				} else {
					destRegions = append(destRegions, region)
				}
			} else {
				foundBucketRegion = true
			}
		}
		if !foundBucketRegion {
			// We don't handle this case and shouldn't ever
			// encounter it
			return nil, fmt.Errorf("BucketRegion %v is not listed in Regions", part.BucketRegion)
		}

		amis := map[string]string{}
		if len(destRegions) > 0 {
			plog.Printf("Replicating AMI %v...", imageID)
			amis, err = api.CopyImage(imageID, destRegions)
			if err != nil {
				return nil, fmt.Errorf("couldn't copy image: %v", err)
			}
		}
		amis[part.BucketRegion] = imageID

		return amis, nil
	}

	hvmAmis, err := postprocess(hvmImageID, false)
	if err != nil {
		return nil, nil, fmt.Errorf("processing HVM images: %v", err)
	}

	pvAmis, err := postprocess(pvImageID, true)
	if err != nil {
		return nil, nil, fmt.Errorf("processing PV images: %v", err)
	}

	return hvmAmis, pvAmis, nil
}

type amiListEntry struct {
	Region string `json:"name"`
	PvAmi  string `json:"pv,omitempty"`
	HvmAmi string `json:"hvm"`
}

type amiList struct {
	Entries []amiListEntry `json:"amis"`
}

func awsUploadAmiLists(ctx context.Context, bucket *storage.Bucket, spec *channelSpec, amis *amiList) error {
	upload := func(name string, data string) error {
		var contentType string
		if strings.HasSuffix(name, ".txt") {
			contentType = "text/plain"
		} else if strings.HasSuffix(name, ".json") {
			contentType = "application/json"
		} else {
			return fmt.Errorf("unknown file extension in %v", name)
		}

		obj := gs.Object{
			Name:        bucket.Prefix() + spec.AWS.Prefix + name,
			ContentType: contentType,
		}
		media := bytes.NewReader([]byte(data))
		if err := bucket.Upload(ctx, &obj, media); err != nil {
			return fmt.Errorf("couldn't upload %v: %v", name, err)
		}
		return nil
	}

	// emit keys in stable order
	sort.Slice(amis.Entries, func(i, j int) bool {
		return amis.Entries[i].Region < amis.Entries[j].Region
	})

	// format JSON AMI list
	var jsonBuf bytes.Buffer
	encoder := json.NewEncoder(&jsonBuf)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(amis); err != nil {
		return fmt.Errorf("couldn't encode JSON: %v", err)
	}
	jsonAll := jsonBuf.String()

	// format text AMI lists and upload AMI IDs for individual regions
	var hvmRecords, pvRecords []string
	for _, entry := range amis.Entries {
		hvmRecords = append(hvmRecords,
			fmt.Sprintf("%v=%v", entry.Region, entry.HvmAmi))
		if entry.PvAmi != "" {
			pvRecords = append(pvRecords,
				fmt.Sprintf("%v=%v", entry.Region, entry.PvAmi))
		}

		if err := upload(fmt.Sprintf("hvm_%v.txt", entry.Region),
			entry.HvmAmi+"\n"); err != nil {
			return err
		}
		if entry.PvAmi != "" {
			if err := upload(fmt.Sprintf("pv_%v.txt", entry.Region),
				entry.PvAmi+"\n"); err != nil {
				return err
			}
			// compatibility
			if err := upload(fmt.Sprintf("%v.txt", entry.Region),
				entry.PvAmi+"\n"); err != nil {
				return err
			}
		}
	}
	hvmAll := strings.Join(hvmRecords, "|") + "\n"
	pvAll := strings.Join(pvRecords, "|") + "\n"

	// upload AMI lists
	if err := upload("all.json", jsonAll); err != nil {
		return err
	}
	if err := upload("hvm.txt", hvmAll); err != nil {
		return err
	}
	if err := upload("pv.txt", pvAll); err != nil {
		return err
	}
	// compatibility
	if err := upload("all.txt", pvAll); err != nil {
		return err
	}

	return nil
}

// awsPreRelease runs everything necessary to prepare a CoreOS release for AWS.
//
// This includes uploading the ami_vmdk image to an S3 bucket in each EC2
// partition, creating HVM and PV AMIs, and replicating the AMIs to each
// region.
func awsPreRelease(ctx context.Context, client *http.Client, src *storage.Bucket, spec *channelSpec, imageInfo *imageInfo) error {
	if spec.AWS.Image == "" {
		plog.Notice("AWS image creation disabled.")
		return nil
	}

	imageName := fmt.Sprintf("%v-%v-%v", spec.AWS.BaseName, specChannel, specVersion)
	imageName = regexp.MustCompile(`[^A-Za-z0-9()\\./_-]`).ReplaceAllLiteralString(imageName, "_")
	imageDescription := fmt.Sprintf("%v %v %v", spec.AWS.BaseDescription, specChannel, specVersion)

	imagePath, err := getImageFile(client, src, spec.AWS.Image)
	if err != nil {
		return err
	}

	var amis amiList
	for i := range spec.AWS.Partitions {
		hvmAmis, pvAmis, err := awsUploadToPartition(spec, &spec.AWS.Partitions[i], imageName, imageDescription, imagePath)
		if err != nil {
			return err
		}

		for region := range hvmAmis {
			amis.Entries = append(amis.Entries, amiListEntry{
				Region: region,
				PvAmi:  pvAmis[region],
				HvmAmi: hvmAmis[region],
			})
		}
	}

	if err := awsUploadAmiLists(ctx, src, spec, &amis); err != nil {
		return fmt.Errorf("uploading AMI IDs: %v", err)
	}

	imageInfo.AWS = &amis
	return nil
}
