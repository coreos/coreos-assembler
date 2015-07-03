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

package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/spf13/cobra"
	"github.com/coreos/mantle/Godeps/_workspace/src/google.golang.org/cloud"
	"github.com/coreos/mantle/Godeps/_workspace/src/google.golang.org/cloud/storage"
	"github.com/coreos/mantle/auth"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/sdk"
)

var (
	cmdUpload = &cobra.Command{

		Use:   "upload",
		Short: "Upload os image",
		Long:  "Upload os image to Google Storage bucket and create image in GCE. Intended for use in SDK.",
		Run:   runUpload,
	}
	uploadBucket    string
	uploadProject   string
	uploadImageName string
	uploadBoard     string
	uploadFile      string
)

func init() {
	build := sdk.BuildRoot()
	cmdUpload.Flags().StringVar(&uploadBucket, "bucket", "gs://users.developer.core-os.net", "gs://bucket/prefix/ prefix defaults to $USER")
	cmdUpload.Flags().StringVar(&uploadProject, "project", "coreos-gce-testing", "Google Compute project ID")
	cmdUpload.Flags().StringVar(&uploadImageName, "name", "", "name for uploaded image, defaults to COREOS_VERSION")
	cmdUpload.Flags().StringVar(&uploadBoard, "board", "amd64-usr", "board used for naming with default prefix only")
	cmdUpload.Flags().StringVar(&uploadFile, "file",
		build+"/images/amd64-usr/latest/coreos_production_gce.tar.gz",
		"path_to_coreos_image (build with: ./image_to_vm.sh --format=gce ...)")
	root.AddCommand(cmdUpload)
}

func runUpload(cmd *cobra.Command, args []string) {
	if len(args) != 0 {
		fmt.Fprintf(os.Stderr, "Unrecognized args in plume upload cmd: %v\n", args)
		os.Exit(2)
	}

	// if an image name is unspecified try to use version.txt
	if uploadImageName == "" {
		uploadImageName = getImageVersion(uploadFile)
		if uploadImageName == "" {
			fmt.Fprintf(os.Stderr, "Unable to get version from image directory, provide a -name flag or include a version.txt in the image directory\n")
			os.Exit(1)
		}
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
	// if prefix not specified default name to gs://bucket/$USER/$BOARD/$VERSION
	if gsURL.Path == "" {
		if user := os.Getenv("USER"); user != "" {
			gsURL.Path = "/" + os.Getenv("USER")
			gsURL.Path += "/" + uploadBoard
		}
	}

	uploadBucket = gsURL.Host
	uploadImageName = strings.TrimPrefix(gsURL.Path+"/"+uploadImageName, "/")
	// create equivalent image names for GS and GCE
	imageNameGCE := gceSanitize(uploadImageName)
	imageNameGS := uploadImageName + ".tar.gz"

	client, err := auth.GoogleClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Authentication failed: %v\n", err)
		os.Exit(1)
	}

	// check if this file is already uploaded and give option to skip
	alreadyExists, err := fileQuery(client, uploadBucket, imageNameGS)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Uploading image failed: %v\n", err)
		os.Exit(1)
	}

	if alreadyExists {
		var ans string
		fmt.Printf("File %v already exists on Google Storage. Overwrite? (y/n):", imageNameGS)
		if _, err = fmt.Scan(&ans); err != nil {
			fmt.Fprintf(os.Stderr, "Scanning overwrite input: %v", err)
			os.Exit(1)
		}
		switch ans {
		case "y", "Y", "yes":
			fmt.Println("Overriding existing file...")
			err = writeFile(client, uploadBucket, uploadFile, imageNameGS)
		default:
			fmt.Println("Skipped file upload")
		}
	} else {
		err = writeFile(client, uploadBucket, uploadFile, imageNameGS)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Uploading image failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Creating image in GCE: %v...\n", imageNameGCE)

	// create image on gce
	storageSrc := fmt.Sprintf("https://storage.googleapis.com/%v/%v", uploadBucket, imageNameGS)
	err = platform.GCECreateImage(client, uploadProject, imageNameGCE, storageSrc)

	// if image already exists ask to delete and try again
	if err != nil && strings.HasSuffix(err.Error(), "alreadyExists") {
		var ans string
		fmt.Printf("Image %v already exists on GCE. Overwrite? (y/n):", imageNameGCE)
		if _, err = fmt.Scan(&ans); err != nil {
			fmt.Fprintf(os.Stderr, "Scanning overwrite input: %v", err)
			os.Exit(1)
		}
		switch ans {
		case "y", "Y", "yes":
			fmt.Println("Overriding existing image...")
			err = platform.GCEForceCreateImage(client, uploadProject, imageNameGCE, storageSrc)

			if err != nil {
				fmt.Fprintf(os.Stderr, "Creating GCE image failed: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Image %v sucessfully created in GCE\n", imageNameGCE)
		default:
			fmt.Println("Skipped GCE image creation")
		}
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Creating GCE image failed: %v\n", err)
		os.Exit(1)
	}

	// ask to additionally update the "latest" image
	var ans string
	fmt.Printf("Would you also like to update the image 'latest'? (y/n):")
	if _, err = fmt.Scan(&ans); err != nil {
		fmt.Fprintf(os.Stderr, "Scanning latest update ans: %v", err)
		os.Exit(1)
	}
	switch ans {
	case "y", "Y", "yes":
		fmt.Println("Updating 'latest' image...")
		err = platform.GCEForceCreateImage(client, uploadProject, "latest", storageSrc)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Updating 'latest' image failed: %v\n", err)
		}
	default:
		fmt.Println("Skipped updating 'latest' image")
	}
}

// Converts an image name from Google Storage to an equivalent GCE image
// name. NOTE: Not a fully generlized sanitizer for GCE. Designed for
// the default version.txt name (ex: 633.1.0+2015-03-31-1538). See:
// https://godoc.org/google.golang.org/api/compute/v1#Image
func gceSanitize(name string) string {
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

// Attempt to get version.txt from image build directory. Return "" if
// unable to retrieve version.txt from directory.
func getImageVersion(path string) string {
	imageDir := filepath.Dir(path)
	b, err := ioutil.ReadFile(filepath.Join(imageDir, "version.txt"))
	if err != nil {
		return ""
	}

	lines := strings.Split(string(b), "\n")
	var version string
	for _, str := range lines {
		if strings.Contains(str, "COREOS_VERSION=") {
			version = strings.TrimPrefix(str, "COREOS_VERSION=")
			break
		}
	}
	return version
}

// Write file to Google Storage
func writeFile(client *http.Client, bucket, filename, destname string) error {
	fmt.Printf("Writing %v to gs://%v ...\n", filename, bucket)
	fmt.Printf("(Sometimes this takes a few mintues)\n")

	// dummy value is used since a project name isn't necessary unless
	// we are creating new buckets
	ctx := cloud.NewContext("dummy", client)
	wc := storage.NewWriter(ctx, bucket, destname)
	wc.ContentType = "application/x-gzip"
	wc.ACL = []storage.ACLRule{{storage.AllAuthenticatedUsers, storage.RoleReader}}

	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(wc, file)
	if err != nil {
		return err
	}
	if err := wc.Close(); err != nil {
		return err
	}

	fmt.Printf("Upload successful!\n")
	return nil
}

// Test if file exists in Google Storage
func fileQuery(client *http.Client, bucket, name string) (bool, error) {
	ctx := cloud.NewContext("dummy", client)
	query := &storage.Query{Prefix: name}

	objects, err := storage.ListObjects(ctx, bucket, query)
	if err != nil {
		return false, err
	}

	if len(objects.Results) == 1 {
		return true, nil
	}

	return false, nil
}
