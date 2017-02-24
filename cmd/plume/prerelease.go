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
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Microsoft/azure-vhd-utils-for-go/vhdcore/validator"
	"github.com/spf13/cobra"
	"golang.org/x/net/context"

	"github.com/coreos/mantle/auth"
	"github.com/coreos/mantle/platform/api/azure"
	"github.com/coreos/mantle/sdk"
	"github.com/coreos/mantle/storage"
	"github.com/coreos/mantle/util"
)

var (
	preReleaseDryRun bool
	cmdPreRelease    = &cobra.Command{
		Use:   "pre-release [options]",
		Short: "Run pre-release steps for CoreOS",
		Long:  "Runs pre-release steps for CoreOS, such as image uploading and OS image creation, and replication across regions.",
		RunE:  runPreRelease,
	}

	azureOpts     = azure.Options{}
	azureProfile  string
	verifyKeyFile string
)

func init() {
	cmdPreRelease.Flags().StringVar(&azureProfile, "azure-profile", "", "Azure Profile json file")
	cmdPreRelease.Flags().BoolVarP(&preReleaseDryRun, "dry-run", "n", false,
		"perform a trial run, do not make changes")
	cmdPreRelease.Flags().StringVar(&verifyKeyFile,
		"verify-key", "", "PGP public key to be used in verifing download signatures.  Defaults to CoreOS Buildbot (0412 7D0B FABE C887 1FFB  2CCE 50E0 8855 93D2 DCB4)")

	AddSpecFlags(cmdPreRelease.Flags())
	root.AddCommand(cmdPreRelease)
}

func runPreRelease(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return errors.New("no args accepted")
	}

	spec := ChannelSpec()
	ctx := context.Background()
	client, err := auth.GoogleClient()
	if err != nil {
		return err
	}

	src, err := storage.NewBucket(client, spec.SourceURL())
	if err != nil {
		return err
	}

	if err := src.Fetch(ctx); err != nil {
		plog.Fatal(err)
	}

	// Sanity check!
	if vertxt := src.Object(src.Prefix() + "version.txt"); vertxt == nil {
		verurl := src.URL().String() + "version.txt"
		plog.Fatalf("File not found: %s", verurl)
	}

	plog.Printf("Running Azure pre-release...")

	if err := azurePreRelease(ctx, client, src, &spec); err != nil {
		return err
	}

	plog.Printf("Pre-release complete, run `plume release` to finish.")

	return nil
}

// getAzureVhd downloads a CoreOS image for Azure and unzips it to vhdPath.
func getAzureVhd(spec *channelSpec, client *http.Client, src *storage.Bucket, bzipPath, vhdPath string) error {
	if _, err := os.Stat(vhdPath); err == nil {
		plog.Printf("Reusing existing image %q", vhdPath)
		return nil
	}

	vhduri, err := url.Parse(spec.Azure.Image)
	if err != nil {
		return err
	}

	vhduri = src.URL().ResolveReference(vhduri)

	plog.Printf("Downloading Azure image %q to %q", vhduri, bzipPath)

	if err := sdk.UpdateSignedFile(bzipPath, vhduri.String(), client, verifyKeyFile); err != nil {
		return err
	}

	// decompress it
	plog.Printf("Decompressing %q...", bzipPath)
	return util.Bunzip2File(vhdPath, bzipPath)
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

	bloburl := api.UrlOfBlob(spec.Azure.StorageAccount, spec.Azure.Containers[0], blobName).String()

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

func replicateAzureImage(api *azure.API, imageName string) error {
	plog.Printf("Fetching Azure Locations...")
	locations, err := api.Locations()
	if err != nil {
		return err
	}

	plog.Printf("Replicating image to locations: %s", strings.Join(locations, ", "))

	channelTitle := strings.Title(specChannel)

	if err := api.ReplicateImage(imageName, "CoreOS", channelTitle, specVersion, locations...); err != nil {
		return fmt.Errorf("image replication failed: %v", err)
	}

	return nil
}

// azurePreRelease runs everything necessary to prepare a CoreOS release for Azure.
//
// This includes uploading the vhd image to Azure storage, creating an OS image from it,
// and replicating that OS image.
func azurePreRelease(ctx context.Context, client *http.Client, src *storage.Bucket, spec *channelSpec) error {
	if spec.Azure.StorageAccount == "" {
		plog.Notice("Azure image creation disabled.")
		return nil
	}

	prof, err := auth.ReadAzureProfile(azureProfile)
	if err != nil {
		return fmt.Errorf("failed reading Azure profile: %v", err)
	}

	for _, opt := range prof.AsOptions() {
		// construct azure api client
		plog.Printf("Creating Azure API from subscription %q endpoint %q", opt.SubscriptionID, opt.ManagementURL)
		api, err := azure.New(&opt)
		if err != nil {
			return fmt.Errorf("failed to create Azure API: %v", err)
		}

		plog.Printf("Fetching Azure storage credentials")

		storageKey, err := api.GetStorageServiceKeys(spec.Azure.StorageAccount)
		if err != nil {
			return err
		}

		// download azure vhd image and unzip it
		cachedir := filepath.Join(sdk.RepoCache(), "images", specChannel, specVersion)
		bzfile := filepath.Join(cachedir, spec.Azure.Image)
		vhdfile := strings.TrimSuffix(bzfile, filepath.Ext(bzfile))
		if err := getAzureVhd(spec, client, src, bzfile, vhdfile); err != nil {
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

		// upload blob, do not overwrite
		plog.Printf("Uploading %q to Azure Storage...", vhdfile)

		blobName := fmt.Sprintf("container-linux-%s-%s.vhd", specVersion, specChannel)

		for _, container := range spec.Azure.Containers {
			blobExists, err := api.BlobExists(spec.Azure.StorageAccount, storageKey.PrimaryKey, container, blobName)
			if err != nil {
				return fmt.Errorf("failed to check if file %q in account %q container %q exists: %v", vhdfile, spec.Azure.StorageAccount, container, err)
			}

			if blobExists {
				continue
			}

			if err := api.UploadBlob(spec.Azure.StorageAccount, storageKey.PrimaryKey, vhdfile, container, blobName, false); err != nil {
				if _, ok := err.(azure.BlobExistsError); !ok {
					return fmt.Errorf("uploading file %q to account %q container %q failed: %v", vhdfile, spec.Azure.StorageAccount, container, err)
				}
			}
		}

		// channel name should be caps for azure image
		imageName := fmt.Sprintf("CoreOS-%s-%s", strings.Title(specChannel), specVersion)

		// create image
		if err := createAzureImage(spec, api, blobName, imageName); err != nil {
			// if it is a conflict, it already exists!
			if !azure.IsConflictError(err) {
				return err
			}

			plog.Printf("Azure image %q already exists", imageName)
		}

		// replicate it
		if err := replicateAzureImage(api, imageName); err != nil {
			return err
		}
	}

	return nil
}
