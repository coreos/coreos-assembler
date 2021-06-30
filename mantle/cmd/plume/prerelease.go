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
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/management/storageservice"
	"github.com/Microsoft/azure-vhd-utils/vhdcore/validator"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/net/context"

	"github.com/coreos/mantle/platform/api/aws"
	"github.com/coreos/mantle/platform/api/azure"
	"github.com/coreos/mantle/sdk"
	"github.com/coreos/mantle/storage"
	"github.com/coreos/mantle/system"
	"github.com/coreos/mantle/util"
)

var (
	cmdPreRelease = &cobra.Command{
		Use:   "pre-release [options]",
		Short: "Run pre-release steps for CoreOS",
		Long:  "Runs pre-release steps for CoreOS, such as image uploading and OS image creation, and replication across regions.",
		RunE:  runPreRelease,

		SilenceUsage: true,
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
	selectedDistro     string
	azureProfile       string
	awsCredentialsFile string
	verifyKeyFile      string
	imageInfoFile      string
)

type imageMetadataAbstract struct {
	Env       string
	Version   string
	Timestamp string
	Respin    string
	ImageType string
	Arch      string
}

type platform struct {
	displayName string
	handler     func(context.Context, *http.Client, *storage.Bucket, *channelSpec, *imageInfo) error
}

type imageInfo struct {
	AWS   *amiList        `json:"aws,omitempty"`
	Azure *azureImageInfo `json:"azure,omitempty"`
}

// Common switches between Fedora Cloud and Fedora CoreOS
func AddSpecFlags(flags *pflag.FlagSet) {
	flags.StringVarP(&specArch, "arch", "A", system.RpmArch(), "target arch")
	flags.StringVarP(&specChannel, "channel", "C", "testing", "target channel")
	if err := flags.MarkDeprecated("channel", "use --stream instead"); err != nil {
		panic(err)
	}
	flags.StringVarP(&specChannel, "stream", "S", "testing", "target stream")
	flags.StringVarP(&specVersion, "version", "V", "", "release version")
}

func init() {
	for k := range platforms {
		platformList = append(platformList, k)
	}
	sort.Strings(platformList)

	cmdPreRelease.Flags().StringSliceVar(&selectedPlatforms, "platform", platformList, "platform to pre-release")
	cmdPreRelease.Flags().StringVar(&selectedDistro, "distro", "fedora", "system to pre-release")
	cmdPreRelease.Flags().StringVar(&azureProfile, "azure-profile", "", "Azure Profile json file")
	cmdPreRelease.Flags().StringVar(&awsCredentialsFile, "aws-credentials", "", "AWS credentials file")
	cmdPreRelease.Flags().StringVar(&verifyKeyFile,
		"verify-key", "", "path to ASCII-armored PGP public key to be used in verifying download signatures.  Defaults to CoreOS Buildbot (0412 7D0B FABE C887 1FFB  2CCE 50E0 8855 93D2 DCB4)")
	cmdPreRelease.Flags().StringVar(&imageInfoFile, "write-image-list", "", "optional output file describing uploaded images")
	AddSpecFlags(cmdPreRelease.Flags())
	AddFedoraSpecFlags(cmdPreRelease.Flags())
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

	switch selectedDistro {
	case "fedora":
		if err := runFedoraPreRelease(cmd); err != nil {
			return err
		}
	default:
		return fmt.Errorf("Unknown distro %q", selectedDistro)
	}
	plog.Printf("Pre-release complete, run `plume release` to finish.")

	return nil
}

func runFedoraPreRelease(cmd *cobra.Command) error {
	spec, err := ChannelFedoraSpec()
	if err != nil {
		return err
	}
	ctx := context.Background()
	client := http.Client{}

	var imageInfo imageInfo

	for _, platformName := range selectedPlatforms {
		platform := platforms[platformName]
		plog.Printf("Running %v pre-release...", platform.displayName)
		if err := platform.handler(ctx, &client, nil, &spec, &imageInfo); err != nil {
			return err
		}
	}

	return nil
}

// getImageFile downloads a bzipped CoreOS image, verifies its signature,
// decompresses it, and returns the decompressed path.
func getImageFile(client *http.Client, spec *channelSpec, src *storage.Bucket, fileName string) (string, error) {
	switch selectedDistro {
	case "fedora":
		return getFedoraImageFile(client, spec, src, fileName)
	default:
		return "", fmt.Errorf("Invalid system: %v", selectedDistro)
	}
}

func getImageTypeURI() string {
	if specImageType == "Cloud-Base" {
		return "Cloud"
	}
	return specImageType
}

func getFedoraImageFile(client *http.Client, spec *channelSpec, src *storage.Bucket, fileName string) (string, error) {
	imagePath := strings.TrimSuffix(fileName, ".xz")

	if _, err := os.Stat(imagePath); err == nil {
		plog.Printf("Reusing existing image %q", imagePath)
		return imagePath, nil
	}

	rawxzURI, err := url.Parse(fmt.Sprintf("%v/%v/compose/%v/%v/images/%v", spec.BaseURL, specComposeID, getImageTypeURI(), specArch, fileName))
	if err != nil {
		return "", err
	}

	plog.Printf("Downloading image %q to %q", rawxzURI, fileName)

	if err := sdk.UpdateFile(fileName, rawxzURI.String(), client); err != nil {
		return "", err
	}

	// decompress it
	plog.Printf("Decompressing %q...", fileName)
	if err := util.XzDecompressFile(imagePath, fileName); err != nil {
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

	// download azure vhd image and unzip it
	vhdfile, err := getImageFile(client, spec, src, spec.Azure.Image)
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
		// construct azure api client
		api, err := azure.New(&azure.Options{
			AzureProfile:      azureProfile,
			AzureSubscription: environment.SubscriptionName,
		})
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

func getSpecAWSImageMetadata(spec *channelSpec) (map[string]string, error) {
	imageFileName := spec.AWS.Image
	imageMetadata := imageMetadataAbstract{
		Env:       specEnv,
		Version:   specVersion,
		Timestamp: specTimestamp,
		Respin:    specRespin,
		ImageType: specImageType,
		Arch:      specArch,
	}
	t := template.Must(template.New("filename").Parse(imageFileName))
	buffer := &bytes.Buffer{}
	if err := t.Execute(buffer, imageMetadata); err != nil {
		return nil, err
	}
	imageFileName = buffer.String()

	var imageName string
	switch selectedDistro {
	case "fedora":
		imageName = strings.TrimSuffix(imageFileName, ".raw.xz")
	}

	imageDescription := fmt.Sprintf("%v %v %v", spec.AWS.BaseDescription, specChannel, specVersion)

	awsImageMetaData := map[string]string{
		"imageFileName":    imageFileName,
		"imageName":        imageName,
		"imageDescription": imageDescription,
	}

	return awsImageMetaData, nil
}

func awsUploadToPartition(spec *channelSpec, part *awsPartitionSpec, imagePath string) (map[string]string, error) {
	plog.Printf("Connecting to %v...", part.Name)
	api, err := aws.New(&aws.Options{
		CredentialsFile: awsCredentialsFile,
		Profile:         part.Profile,
		Region:          part.BucketRegion,
	})
	if err != nil {
		return nil, fmt.Errorf("creating client for %v: %v", part.Name, err)
	}

	f, err := os.Open(imagePath)
	if err != nil {
		return nil, fmt.Errorf("Could not open image file %v: %v", imagePath, err)
	}
	defer f.Close()

	awsImageMetadata, err := getSpecAWSImageMetadata(spec)
	if err != nil {
		return nil, fmt.Errorf("Could not generate the image metadata: %v", err)
	}

	imageFileName := awsImageMetadata["imageFileName"]
	imageName := awsImageMetadata["imageName"]
	imageDescription := awsImageMetadata["imageDescription"]

	var s3ObjectPath string
	switch selectedDistro {
	case "fedora":
		s3ObjectPath = fmt.Sprintf("%s/%s/%s", specArch, specVersion, strings.TrimSuffix(imageFileName, filepath.Ext(imageFileName)))
	}
	s3ObjectURL := fmt.Sprintf("s3://%s/%s", part.Bucket, s3ObjectPath)

	snapshot, err := api.FindSnapshot(imageName)
	if err != nil {
		return nil, fmt.Errorf("unable to check for snapshot: %v", err)
	}

	if snapshot == nil {
		plog.Printf("Creating S3 object %v...", s3ObjectURL)
		err = api.UploadObject(f, part.Bucket, s3ObjectPath, false)
		if err != nil {
			return nil, fmt.Errorf("Error uploading: %v", err)
		}

		plog.Printf("Creating EBS snapshot...")

		var format aws.EC2ImageFormat
		switch selectedDistro {
		case "fedora":
			format = aws.EC2ImageFormatRaw
		}

		snapshot, err = api.CreateSnapshot(imageName, s3ObjectURL, format)
		if err != nil {
			return nil, fmt.Errorf("unable to create snapshot: %v", err)
		}
	}

	// delete unconditionally to avoid leaks after a restart
	plog.Printf("Deleting S3 object %v...", s3ObjectURL)
	err = api.DeleteObject(part.Bucket, s3ObjectPath)
	if err != nil {
		return nil, fmt.Errorf("Error deleting S3 object: %v", err)
	}

	plog.Printf("Creating AMIs from %v...", snapshot.SnapshotID)

	imageID, err := api.CreateHVMImage(snapshot.SnapshotID, aws.ContainerLinuxDiskSizeGiB, imageName, imageDescription, "x86_64")
	if err != nil {
		return nil, fmt.Errorf("unable to create image: %v", err)
	}
	resources := []string{snapshot.SnapshotID, imageID}

	switch selectedDistro {
	case "fedora":
		err = api.CreateTags(resources, map[string]string{
			"Channel":   specChannel,
			"Version":   specVersion,
			"ComposeID": specComposeID,
			"Date":      specTimestamp,
			"Arch":      specArch,
		})
		if err != nil {
			return nil, fmt.Errorf("couldn't tag images: %v", err)
		}
	}

	if len(part.LaunchPermissions) > 0 {
		if err := api.GrantLaunchPermission(imageID, part.LaunchPermissions); err != nil {
			return nil, err
		}
	}

	destRegions := make([]string, 0, len(part.Regions))
	foundBucketRegion := false
	for _, region := range part.Regions {
		if region != part.BucketRegion {
			destRegions = append(destRegions, region)
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
		err := api.CopyImage(imageID, destRegions, func(region string, ami aws.ImageData) {
			amis[region] = ami.AMI
		})
		if err != nil {
			return nil, fmt.Errorf("couldn't copy image: %v", err)
		}
	}
	amis[part.BucketRegion] = imageID

	return amis, nil
}

type amiListEntry struct {
	Region string `json:"name"`
	Ami    string `json:"hvm"`
}

type amiList struct {
	Entries []amiListEntry `json:"amis"`
}

// awsPreRelease runs everything necessary to prepare a CoreOS release for AWS.
//
// This includes uploading the ami_vmdk image to an S3 bucket in each EC2
// partition, creating AMIs, and replicating the AMIs to each region.
func awsPreRelease(ctx context.Context, client *http.Client, src *storage.Bucket, spec *channelSpec, imageInfo *imageInfo) error {
	if spec.AWS.Image == "" {
		plog.Notice("AWS image creation disabled.")
		return nil
	}

	awsImageMetadata, err := getSpecAWSImageMetadata(spec)
	if err != nil {
		return fmt.Errorf("Could not generate the image filname: %v", err)
	}

	imageFileName := awsImageMetadata["imageFileName"]

	imagePath, err := getImageFile(client, spec, src, imageFileName)
	if err != nil {
		return err
	}

	var amis amiList
	for i := range spec.AWS.Partitions {
		amiMap, err := awsUploadToPartition(spec, &spec.AWS.Partitions[i], imagePath)
		if err != nil {
			return err
		}

		for region := range amiMap {
			amis.Entries = append(amis.Entries, amiListEntry{
				Region: region,
				Ami:    amiMap[region],
			})
		}
	}

	imageInfo.AWS = &amis
	return nil
}
