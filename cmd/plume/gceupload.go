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
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/spf13/cobra"
	"github.com/coreos/mantle/Godeps/_workspace/src/google.golang.org/cloud"
	"github.com/coreos/mantle/Godeps/_workspace/src/google.golang.org/cloud/storage"
	"github.com/coreos/mantle/auth"
	"github.com/coreos/mantle/sdk"
)

var (
	cmdGCEUpload = &cobra.Command{
		Use:   "gce-upload",
		Short: "Upload gce image",
		Long:  "Upload os image to Google Storage bucket and create image in GCE. Intended for use in SDK.",
		Run:   runGCEUpload,
	}
	gceUploadForce     bool
	gceUploadRetries   int
	gceUploadBucket    string
	gceUploadImageName string
	gceUploadBoard     string
	gceUploadFile      string
)

func init() {
	build := sdk.BuildRoot()
	cmdGCEUpload.Flags().BoolVar(&gceUploadForce, "force", false, "set true to overwrite existing image with same name")
	cmdGCEUpload.Flags().IntVar(&gceUploadRetries, "set-retries", 0, "set how many times to retry on failure")
	cmdGCEUpload.Flags().StringVar(&gceUploadBucket, "bucket", "gs://users.developer.core-os.net", "gs://bucket/prefix/ prefix defaults to $USER")
	cmdGCEUpload.Flags().StringVar(&gceUploadImageName, "name", "", "name for uploaded image, defaults to COREOS_VERSION")
	cmdGCEUpload.Flags().StringVar(&gceUploadBoard, "board", "amd64-usr", "board used for naming with default prefix only")
	cmdGCEUpload.Flags().StringVar(&gceUploadFile, "file",
		build+"/images/amd64-usr/latest/coreos_production_gce.tar.gz",
		"path_to_coreos_image (build with: ./image_to_vm.sh --format=gce ...)")
	root.AddCommand(cmdGCEUpload)
}

func runGCEUpload(cmd *cobra.Command, args []string) {
	if len(args) != 0 {
		log.Printf("Unrecognized args in gce-upload cmd: %v\n", args)
		os.Exit(2)
	}

	// if an image name is unspecified try to use version.txt
	if gceUploadImageName == "" {
		gceUploadImageName = getImageVersion(gceUploadFile)
		if gceUploadImageName == "" {
			log.Printf("Unable to get version from image directory, provide a -name flag or include a version.txt in the image directory\n")
			os.Exit(1)
		}
	}

	gsURL, err := url.Parse(gceUploadBucket)
	if err != nil {
		log.Printf("%v\n", err)
		os.Exit(1)
	}
	if gsURL.Scheme != "gs" {
		log.Printf("URL missing gs:// scheme prefix: %v\n", gceUploadBucket)
		os.Exit(1)
	}
	if gsURL.Host == "" {
		log.Printf("URL missing bucket name %v\n", gceUploadBucket)
		os.Exit(1)
	}
	// if prefix not specified default name to gs://bucket/$USER/$BOARD/$VERSION
	if gsURL.Path == "" {
		if user := os.Getenv("USER"); user != "" {
			gsURL.Path = "/" + os.Getenv("USER")
			gsURL.Path += "/" + gceUploadBoard
		}
	}

	uploadBucket := gsURL.Host
	imageNameGS := strings.TrimPrefix(gsURL.Path+"/"+gceUploadImageName, "/") + ".tar.gz"

	var retries int
	if gceUploadRetries > 0 {
		retries = gceUploadRetries
	} else {
		retries = 1
	}

	var returnVal int
	for i := 0; i < retries; i++ {
		if i > 0 {
			log.Printf("trying again...")
		}

		returnVal = tryGCEUpload(uploadBucket, imageNameGS)
		if returnVal == 0 {
			os.Exit(0)
		}
	}
	os.Exit(returnVal)
}

func tryGCEUpload(uploadBucket, imageNameGS string) int {
	client, err := auth.GoogleClient()
	if err != nil {
		log.Printf("Authentication failed: %v\n", err)
		os.Exit(1)
	}

	// check if this file is already uploaded
	alreadyExists, err := fileQuery(client, uploadBucket, imageNameGS)
	if err != nil {
		log.Printf("Uploading image failed: %v\n", err)
		return 1
	}

	if alreadyExists {
		if !gceUploadForce {
			log.Printf("skipping upload, gs://%v/%v already exists on Google Storage.", uploadBucket, imageNameGS)
			return 0
		}

		log.Println("forcing image upload...")
	}

	err = writeFile(client, uploadBucket, gceUploadFile, imageNameGS)
	if err != nil {
		log.Printf("Uploading image failed: %v\n", err)
		return 1
	}
	log.Printf("wrote gs://%v/%v", uploadBucket, imageNameGS)

	return 0
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
	log.Printf("Writing %v to gs://%v ...\n", filename, bucket)
	log.Printf("(Sometimes this takes a few mintues)\n")

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

	log.Printf("Upload successful!\n")
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
