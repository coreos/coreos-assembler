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
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/coreos/mantle/Godeps/_workspace/src/google.golang.org/api/compute/v1"
	"github.com/coreos/mantle/Godeps/_workspace/src/google.golang.org/cloud"
	"github.com/coreos/mantle/Godeps/_workspace/src/google.golang.org/cloud/storage"
	"github.com/coreos/mantle/auth"
	"github.com/coreos/mantle/cli"
)

var (
	cmdUpload = &cli.Command{

		Name:        "upload",
		Summary:     "Upload os image",
		Usage:       "plume upload",
		Description: "Upload os image to Google Storage bucket and create image in GCE. Intended for use in SDK.",
		Flags:       *flag.NewFlagSet("upload", flag.ExitOnError),
		Run:         runUpload,
	}
	bucket       string
	imagePath    string
	imageName    string
	gceProjectID string
	board        string
)

func init() {
	cmdUpload.Flags.StringVar(&bucket, "bucket", "gs://users.developer.core-os.net", "gs://bucket/prefix/ prefix defaults to $USER")
	cmdUpload.Flags.StringVar(&gceProjectID, "gce-project", "coreos-gce-testing", "Google Compute project ID")
	cmdUpload.Flags.StringVar(&imageName, "name", "", "name for uploaded image, defaults to COREOS_VERSION")
	cmdUpload.Flags.StringVar(&imagePath, "image",
		"/mnt/host/source/src/build/images/amd64-usr/latest/coreos_production_gce.tar.gz",
		"path_to_coreos_image (build with: ./image_to_vm.sh --format=gce ...)")
	cmdUpload.Flags.StringVar(&board, "board", "amd64-usr", "board used for naming with default prefix only")
	cli.Register(cmdUpload)
}

func runUpload(args []string) int {
	if len(args) != 0 {
		fmt.Fprintf(os.Stderr, "Unrecognized args in plume upload cmd: %v\n", args)
		return 2
	}

	// if an image name is unspecified try to use version.txt
	if imageName == "" {
		imageName = getImageVersion(imagePath)
		if imageName == "" {
			fmt.Fprintf(os.Stderr, "Unable to get version from image directory, provide a -name flag or include a version.txt in the image directory\n")
			return 1
		}
	}

	gsURL, err := url.Parse(bucket)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	if gsURL.Scheme != "gs" {
		fmt.Fprintf(os.Stderr, "URL missing gs:// scheme prefix: %v\n", bucket)
		return 1
	}
	if gsURL.Host == "" {
		fmt.Fprintf(os.Stderr, "URL missing bucket name %v\n", bucket)
		return 1
	}
	// if prefix not specified default name to gs://bucket/$USER/$BOARD/$VERSION
	if gsURL.Path == "" {
		if user := os.Getenv("USER"); user != "" {
			gsURL.Path = "/" + os.Getenv("USER")
			gsURL.Path += "/" + board
		}
	}

	bucket = gsURL.Host
	imageName = strings.TrimPrefix(gsURL.Path+"/"+imageName, "/")
	// create equivalent image names for GS and GCE
	imageNameGCE := gceSanitize(imageName)
	imageNameGS := imageName + ".tar.gz"

	client, err := auth.GoogleClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Authentication failed: %v\n", err)
		return 1
	}

	fmt.Printf("Writing %v to gs://%v ...\n", imageNameGS, bucket)
	fmt.Printf("(Sometimes this takes a few mintues)\n")

	if err := writeFile(client, imagePath, imageNameGS); err != nil {
		fmt.Fprintf(os.Stderr, "Uploading image failed: %v\n", err)
		return 1
	}

	fmt.Printf("Upload successful!\n")
	fmt.Printf("Creating image in GCE: %v...\n", imageNameGCE)

	// create image on gce
	storageSrc := fmt.Sprintf("https://storage.googleapis.com/%v/%v", bucket, imageNameGS)
	err = createImage(client, gceProjectID, imageNameGCE, storageSrc)

	// if image already exists ask to delete and try again
	if err != nil && strings.HasSuffix(err.Error(), "alreadyExists") {
		var ans string
		fmt.Printf("Image %v already exists on GCE. Overwrite? (y/n):", imageNameGCE)
		if _, err = fmt.Scan(&ans); err != nil {
			fmt.Fprintf(os.Stderr, "Scanning overwrite input: %v", err)
			return 1
		}
		switch ans {
		case "y", "Y", "yes":
			fmt.Println("Overriding existing image...")
			err = forceCreateImage(client, gceProjectID, imageNameGCE, storageSrc)
		default:
			fmt.Println("Skipped GCE image creation")
			return 0
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Creating GCE image failed: %v\n", err)
		return 1
	}
	fmt.Printf("Image %v sucessfully created in GCE\n", imageNameGCE)

	return 0
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
func getImageVersion(imagePath string) string {
	imageDir := filepath.Dir(imagePath)
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
func writeFile(client *http.Client, filename, destname string) error {
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

	return nil
}

// Create image on GCE and return. Will not overwrite existing image.
func createImage(client *http.Client, proj, name, source string) error {
	computeService, err := compute.New(client)
	if err != nil {
		return err
	}
	imageService := compute.NewImagesService(computeService)
	image := &compute.Image{
		Name: name,
		RawDisk: &compute.ImageRawDisk{
			Source: source,
		},
	}
	_, err = imageService.Insert(proj, image).Do()
	if err != nil {
		return err
	}
	return nil
}

// Delete image on GCE and then recreate it.
func forceCreateImage(client *http.Client, proj, name, source string) error {
	// delete
	computeService, err := compute.New(client)
	if err != nil {
		return fmt.Errorf("deleting image: %v", err)
	}
	imageService := compute.NewImagesService(computeService)
	_, err = imageService.Delete(proj, name).Do()
	if err != nil {
		return fmt.Errorf("deleting image: %v", err)
	}

	// create
	return createImage(client, proj, name, source)
}
