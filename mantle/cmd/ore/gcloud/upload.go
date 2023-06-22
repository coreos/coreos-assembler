// Copyright 2015 CoreOS, Inc.
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

package gcloud

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	"google.golang.org/api/storage/v1"

	"github.com/coreos/coreos-assembler/mantle/platform/api/gcloud"
)

var (
	cmdUpload = &cobra.Command{
		Use:   "upload",
		Short: "Upload os image",
		Long:  "Upload os image to Google Storage bucket and create image in GCP. Intended for use in SDK.",
		Run:   runUpload,
	}

	uploadBucket           string
	uploadImageName        string
	uploadFile             string
	uploadForce            bool
	uploadWriteUrl         string
	uploadImageArch        string
	uploadImageFamily      string
	uploadImageDescription string
	uploadCreateImage      bool
	uploadPublic           bool
	uploadImageLicenses    []string
)

func init() {
	cmdUpload.Flags().StringVar(&uploadBucket, "bucket", "", "gs://bucket/prefix/")
	if err := cmdUpload.MarkFlagRequired("bucket"); err != nil {
		panic(err)
	}
	cmdUpload.Flags().StringVar(&uploadImageName, "name", "", "name for uploaded image")
	if err := cmdUpload.MarkFlagRequired("name"); err != nil {
		panic(err)
	}
	cmdUpload.Flags().StringVar(&uploadFile, "file", "", "path to image .tar.gz file to upload")
	if err := cmdUpload.MarkFlagRequired("file"); err != nil {
		panic(err)
	}
	cmdUpload.Flags().BoolVar(&uploadForce, "force", false, "overwrite existing GS and GCP images without prompt")
	cmdUpload.Flags().StringVar(&uploadWriteUrl, "write-url", "", "output the uploaded URL to the named file")
	cmdUpload.Flags().StringVar(&uploadImageArch, "arch", "", "The architecture of the image")
	cmdUpload.Flags().StringVar(&uploadImageDescription, "description", "", "The description that should be attached to the image")
	cmdUpload.Flags().BoolVar(&uploadCreateImage, "create-image", true, "Create an image in GCP after uploading")
	cmdUpload.Flags().BoolVar(&uploadPublic, "public", false, "Set public ACLs on image")
	cmdUpload.Flags().StringSliceVar(
		&uploadImageLicenses, "license", []string{},
		"License to attach to image. Can be specified multiple times.")
	GCloud.AddCommand(cmdUpload)
}

func runUpload(cmd *cobra.Command, args []string) {
	if len(args) != 0 {
		fmt.Fprintf(os.Stderr, "Unrecognized args in plume upload cmd: %v\n", args)
		os.Exit(2)
	}

	gsURL, err := url.Parse(uploadBucket)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	if gsURL.Scheme != "gs" {
		fmt.Fprintf(os.Stderr, "URL missing gs:// scheme prefix: %v\n", uploadBucket)
		os.Exit(1)
	}
	if gsURL.Host == "" {
		fmt.Fprintf(os.Stderr, "URL missing bucket name %v\n", uploadBucket)
		os.Exit(1)
	}
	if gsURL.Path == "" {
		fmt.Fprint(os.Stderr, "prefix not specified. Refusing to upload in root directory of bucket\n")
		os.Exit(1)
	}

	uploadBucket = gsURL.Host
	imageNameGS := strings.TrimPrefix(gsURL.Path+"/"+uploadImageName, "/") + ".tar.gz"

	// Sanitize the image name for GCP
	imageNameGCP := gcpSanitize(uploadImageName)

	ctx := context.Background()
	storageAPI, err := storage.NewService(ctx, option.WithHTTPClient(api.Client()))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Storage client failed: %v\n", err)
		os.Exit(1)
	}

	// check if this file is already uploaded and give option to skip
	alreadyExists, err := fileQuery(storageAPI, uploadBucket, imageNameGS)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Uploading image failed: %v\n", err)
		os.Exit(1)
	}

	if alreadyExists && !uploadForce {
		var ans string
		fmt.Printf("File %v already exists on Google Storage. Overwrite? (y/n):", imageNameGS)
		if _, err = fmt.Scan(&ans); err != nil {
			fmt.Fprintf(os.Stderr, "Scanning overwrite input: %v", err)
			os.Exit(1)
		}
		switch ans {
		case "y", "Y", "yes":
			fmt.Println("Overriding existing file...")
			err = writeFile(storageAPI, uploadBucket, uploadFile, imageNameGS)
		default:
			fmt.Println("Skipped file upload")
		}
	} else {
		err = writeFile(storageAPI, uploadBucket, uploadFile, imageNameGS)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Uploading image failed: %v\n", err)
		os.Exit(1)
	}

	imageStorageURL := fmt.Sprintf("https://storage.googleapis.com/%v/%v", uploadBucket, imageNameGS)

	if uploadCreateImage {
		fmt.Printf("Creating image in GCP: %v...\n", imageNameGCP)
		spec := &gcloud.ImageSpec{
			Architecture: uploadImageArch,
			Name:         imageNameGCP,
			Family:       uploadImageFamily,
			SourceImage:  imageStorageURL,
			Description:  uploadImageDescription,
		}
		if len(uploadImageLicenses) > 0 {
			spec.Licenses = uploadImageLicenses
		}
		_, pending, err := api.CreateImage(spec, uploadForce)
		if err == nil {
			err = pending.Wait()
		}

		// if image already exists ask to delete and try again
		if err != nil && strings.HasSuffix(err.Error(), "alreadyExists") {
			var ans string
			fmt.Printf("Image %v already exists on GCP. Overwrite? (y/n):", imageNameGCP)
			if _, err = fmt.Scan(&ans); err != nil {
				fmt.Fprintf(os.Stderr, "Scanning overwrite input: %v", err)
				os.Exit(1)
			}
			switch ans {
			case "y", "Y", "yes":
				fmt.Println("Overriding existing image...")
				_, pending, err := api.CreateImage(spec, true)
				if err == nil {
					err = pending.Wait()
				}
				if err != nil {
					fmt.Fprintf(os.Stderr, "Creating GCP image failed: %v\n", err)
					os.Exit(1)
				}
				fmt.Printf("Image %v sucessfully created in GCP\n", imageNameGCP)
			default:
				fmt.Println("Skipped GCP image creation")
			}
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Creating GCP image failed: %v\n", err)
			os.Exit(1)
		}

		// If requested, set the image ACL to public
		if uploadPublic {
			fmt.Printf("Setting image to have public access: %v\n", imageNameGCP)
			err = api.SetImagePublic(imageNameGCP)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Marking GCP image with public ACLs failed: %v\n", err)
				os.Exit(1)
			}
		}
	}

	if uploadWriteUrl != "" {
		err = os.WriteFile(uploadWriteUrl, []byte(imageStorageURL), 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Writing file (%v) failed: %v\n", uploadWriteUrl, err)
			os.Exit(1)
		}
	}

}

// Converts an image name from Google Storage to an equivalent GCP image
// name. NOTE: Not a fully generlized sanitizer for GCP. Designed for
// the default version.txt name (ex: 633.1.0+2015-03-31-1538). See:
// https://godoc.org/google.golang.org/api/compute/v1#Image
func gcpSanitize(name string) string {
	if name == "" {
		return name
	}

	// remove incompatible chars from version.txt
	name = strings.Replace(name, ".", "-", -1)
	name = strings.Replace(name, "+", "-", -1)

	// remove forward slashes likely from prefix
	name = strings.Replace(name, "/", "-", -1)

	// ensure name starts with [a-z]
	char := name[0]
	if char >= 'a' && char <= 'z' {
		return name
	}
	if char >= 'A' && char <= 'Z' {
		return strings.ToLower(name[:1]) + name[1:]
	}
	return "v" + name
}

// Write file to Google Storage
func writeFile(api *storage.Service, bucket, filename, destname string) error {
	fmt.Printf("Writing %v to gs://%v/%v ...\n", filename, bucket, destname)
	fmt.Printf("(Sometimes this takes a few minutes)\n")

	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	req := api.Objects.Insert(bucket, &storage.Object{
		Name:        destname,
		ContentType: "application/x-gzip",
	})
	req.PredefinedAcl("authenticatedRead")
	req.Media(file)

	if _, err := req.Do(); err != nil {
		return err
	}

	fmt.Printf("Upload successful!\n")
	return nil
}

// Test if file exists in Google Storage
func fileQuery(api *storage.Service, bucket, name string) (bool, error) {
	req := api.Objects.Get(bucket, name)
	if _, err := req.Do(); err != nil {
		if e, ok := err.(*googleapi.Error); ok && e.Code == 404 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
