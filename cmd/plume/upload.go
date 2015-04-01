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

	"github.com/coreos/mantle/auth"
	"github.com/coreos/mantle/cli"
	"google.golang.org/cloud"
	"google.golang.org/cloud/storage"
)

var (
	cmdIndex = &cli.Command{

		Name:        "upload",
		Summary:     "Upload os image",
		Usage:       "-bucket gs://bucket/prefix/ -image filepath",
		Description: "Upload os image to Google Storage bucket",
		Flags:       *flag.NewFlagSet("upload", flag.ExitOnError),
		Run:         runUpload,
	}
	bucket    string
	image     string
	imageName string
	projectID string
)

func init() {
	cmdIndex.Flags.StringVar(&bucket, "bucket", "gs://coreos-plume", "gs://bucket/prefix/")
	cmdIndex.Flags.StringVar(&projectID, "projectID", "coreos-gce-testing", "found in developers console")
	cmdIndex.Flags.StringVar(&imageName, "name", "", "filename for uploaded image, defaults to COREOS_VERSION")
	cmdIndex.Flags.StringVar(&image, "image",
		"/mnt/host/source/src/build/images/amd64-usr/latest/coreos_production_gce.tar.gz",
		"path_to_coreos_image (build with: ./image_to_vm.sh --format=gce ...)")
	cli.Register(cmdIndex)
}

func runUpload(args []string) int {
	if len(args) != 0 {
		fmt.Fprintf(os.Stderr, "Unrecognized args in plume upload cmd: %v\n", args)
		return 2
	}

	// if an image name is unspecified try to use version.txt
	if imageName == "" {
		imageName = getImageVersion(image)
		if imageName == "" {
			fmt.Fprintf(os.Stderr, "Unable to get version from image directory, provide a -name flag or include version.txt in the image directory\n")
			return 1
		}
	}
	imageName += ".tar.gz"

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
	bucket = gsURL.Host
	imageName = strings.TrimPrefix(gsURL.Path+"/"+imageName, "/")

	client, err := auth.GoogleClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Authentication failed: %v\n", err)
		return 1
	}

	fmt.Printf("Writing %v to %v\n", imageName, bucket)

	if err := writeFile(client, imageName); err != nil {
		fmt.Fprintf(os.Stderr, "Uploading image failed: %v\n", err)
		return 1
	}

	fmt.Printf("Update successful!\n")
	return 0
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

func writeFile(client *http.Client, filename string) error {
	ctx := cloud.NewContext(projectID, client)
	wc := storage.NewWriter(ctx, bucket, filename)
	wc.ContentType = "application/x-gzip"
	wc.ACL = []storage.ACLRule{{storage.AllAuthenticatedUsers, storage.RoleReader}}

	imageFile, err := os.Open(image)
	if err != nil {
		return err
	}
	_, err = io.Copy(wc, imageFile)
	if err != nil {
		return err
	}
	if err := wc.Close(); err != nil {
		return err
	}

	return nil
}
