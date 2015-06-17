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
	"log"
	"net/url"
	"strings"

	"github.com/coreos/mantle/auth"
	"github.com/coreos/mantle/cli"
	"github.com/coreos/mantle/platform"
)

var (
	cmdGCECreate = &cli.Command{

		Name:        "gce-create",
		Summary:     "Create gce image",
		Usage:       "",
		Description: "Create GCE image from os image in Google Storage bucket.",
		Flags:       *flag.NewFlagSet("gceCreate", flag.ExitOnError),
		Run:         runGCECreate,
	}
	gceCreateForce     bool
	gceCreateFile      string
	gceCreateProject   string
	gceCreateImageName string
)

func init() {
	cmdGCECreate.Flags.BoolVar(&gceCreateForce, "force", false, "set true to overwrite existing image with same name")
	cmdGCECreate.Flags.StringVar(&gceCreateFile, "from-storage", "", "file from a google storage bucket <gs://bucket/prefix/name>")
	cmdGCECreate.Flags.StringVar(&gceCreateProject, "project", "coreos-gce-testing", "Google Compute project ID")
	cmdGCECreate.Flags.StringVar(&gceCreateImageName, "name", "", "name for uploaded image, defaults to translating the filename in the bucket")

	cli.Register(cmdGCECreate)
}

func runGCECreate(args []string) int {
	if len(args) != 0 {
		log.Printf("Unrecognized args in gce-create cmd: %v\n", args)
		return 2
	}

	if gceCreateFile == "" {
		log.Printf("must specify 'from-storage' flag with a storage bucket")
	}

	gsURL, err := url.Parse(gceCreateFile)
	if err != nil {
		log.Printf("%v", err)
		return 1
	}
	if gsURL.Scheme != "gs" {
		log.Printf("URL missing gs:// scheme prefix: %v\n", gceCreateFile)
		return 1
	}
	if gsURL.Host == "" {
		log.Printf("URL missing bucket name %v\n", gceCreateFile)
		return 1
	}
	if gsURL.Path == "" {
		log.Printf("URL missing filepath: %v", gceCreateFile)
		return 1
	}

	if gceCreateImageName == "" {
		path := strings.TrimSuffix(gsURL.Path, ".tar.gz")
		gceCreateImageName = gceSanitize(strings.TrimPrefix(path, "/"))
	}

	bucket := gsURL.Host
	bucketPath := strings.TrimPrefix(gsURL.Path, "/")
	imageName := gceCreateImageName

	client, err := auth.GoogleClient()
	if err != nil {
		log.Printf("Authentication failed: %v", err)
		return 1
	}

	// make sure file exists
	exists, err := fileQuery(client, bucket, bucketPath)
	if err != nil || !exists {
		log.Printf("failed to find existance of storage image: %v", err)
		return 1
	}

	log.Printf("Creating image in GCE: %v...\n", imageName)

	// create image on gce
	storageSrc := fmt.Sprintf("https://storage.googleapis.com/%v/%v", bucket, bucketPath)
	err = platform.GCECreateImage(client, gceCreateProject, imageName, storageSrc)

	// if image already exists ask to delete and try again
	if err != nil && strings.HasSuffix(err.Error(), "alreadyExists") {
		if gceCreateForce {
			log.Println("forcing overwrite of existing image...")
			err = platform.GCEForceCreateImage(client, gceCreateProject, imageName, storageSrc)
		} else {
			log.Printf("skipping upload, image %v already exists", imageName)
			return 0
		}
	}

	if err != nil {
		log.Printf("Creating GCE image failed: %v", err)
		return 1
	}

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
