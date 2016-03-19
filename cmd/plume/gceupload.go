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
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/spf13/cobra"
	"github.com/coreos/mantle/Godeps/_workspace/src/google.golang.org/api/googleapi"
	"github.com/coreos/mantle/Godeps/_workspace/src/google.golang.org/api/storage/v1"
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
	gceUploadBucket    string
	gceUploadImageName string
	gceUploadBoard     string
	gceUploadFile      string
)

func init() {
	build := sdk.BuildRoot()
	cmdGCEUpload.Flags().BoolVar(&gceUploadForce, "force", false, "set true to overwrite existing image with same name")
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

	client, err := auth.GoogleClient()
	if err != nil {
		log.Printf("Authentication failed: %v\n", err)
		os.Exit(1)
	}

	storageAPI, err := storage.New(client)
	if err != nil {
		log.Printf("Storage client failed: %v\n", err)
		os.Exit(1)
	}

	// check if this file is already uploaded
	alreadyExists, err := fileQuery(storageAPI, uploadBucket, imageNameGS)
	if err != nil {
		log.Printf("Uploading image failed: %v\n", err)
		os.Exit(1)
	}

	if alreadyExists {
		if !gceUploadForce {
			log.Printf("skipping upload, gs://%v/%v already exists on Google Storage.", uploadBucket, imageNameGS)
			os.Exit(0)
		}

		log.Println("forcing image upload...")
	}

	err = writeFile(storageAPI, uploadBucket, gceUploadFile, imageNameGS)
	if err != nil {
		log.Printf("Uploading image failed: %v\n", err)
		os.Exit(1)
	}
	log.Printf("wrote gs://%v/%v", uploadBucket, imageNameGS)
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
func writeFile(api *storage.Service, bucket, filename, destname string) error {
	log.Printf("Writing %v to gs://%v ...\n", filename, bucket)
	log.Printf("(Sometimes this takes a few mintues)\n")

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

	log.Printf("Upload successful!\n")
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
