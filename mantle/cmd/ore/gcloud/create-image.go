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
	"path"
	"strings"

	"github.com/spf13/cobra"
	"google.golang.org/api/storage/v1"

	"github.com/coreos/coreos-assembler/mantle/platform/api/gcloud"
)

var (
	cmdCreateImage = &cobra.Command{
		Use:   "create-image",
		Short: "Create GCE image",
		Long:  "Create GCE image from an existing file in Google Storage",
		Run:   runCreateImage,
	}

	createImageFamily  string
	createImageBoard   string
	createImageVersion string
	createImageRoot    string
	createImageName    string
	createImageForce   bool
)

func init() {
	user := os.Getenv("USER")
	cmdCreateImage.Flags().StringVar(&createImageFamily, "family",
		user, "GCE image group and name prefix")
	cmdCreateImage.Flags().StringVar(&createImageBoard, "board",
		"amd64-usr", "OS board name")
	cmdCreateImage.Flags().StringVar(&createImageVersion, "version",
		"", "OS build version")
	cmdCreateImage.Flags().StringVar(&createImageRoot, "source-root",
		"gs://users.developer.core-os.net/"+user+"/boards",
		"Storage URL prefix")
	cmdCreateImage.Flags().StringVar(&createImageName, "source-name",
		"coreos_production_gce.tar.gz",
		"Storage image name")
	cmdCreateImage.Flags().BoolVar(&createImageForce, "force",
		false, "overwrite existing GCE images without prompt")
	GCloud.AddCommand(cmdCreateImage)
}

func runCreateImage(cmd *cobra.Command, args []string) {
	if len(args) != 0 {
		fmt.Fprintf(os.Stderr, "Unrecognized args: %v\n", args)
		os.Exit(2)
	}

	if createImageVersion == "" {
		fmt.Fprintln(os.Stderr, "--version is required")
		os.Exit(2)
	}

	gsURL, err := url.Parse(createImageRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	if gsURL.Scheme != "gs" {
		fmt.Fprintf(os.Stderr, "URL missing gs:// scheme: %v\n", createImageRoot)
		os.Exit(1)
	}
	if gsURL.Host == "" {
		fmt.Fprintf(os.Stderr, "URL missing bucket name %v\n", createImageRoot)
		os.Exit(1)
	}

	bucket := gsURL.Host
	imageNameGS := strings.TrimPrefix(path.Join(gsURL.Path,
		createImageBoard, createImageVersion, createImageName), "/")
	imageNameGCE := gceSanitize(createImageFamily + "-" + createImageVersion)

	ctx := context.Background()
	storageAPI, err := storage.NewService(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Storage client failed: %v\n", err)
		os.Exit(1)
	}

	// check if this file actually exists
	if ok, err := fileQuery(storageAPI, bucket, imageNameGS); err != nil {
		fmt.Fprintf(os.Stderr,
			"Checking source image %s failed: %v\n", gsURL, err)
		os.Exit(1)
	} else if !ok {
		fmt.Fprintf(os.Stderr,
			"Source image %s does not exist\n", gsURL)
		os.Exit(1)
	}

	fmt.Printf("Creating image in GCE: %v...\n", imageNameGCE)

	// create image on gce
	storageSrc := fmt.Sprintf("https://storage.googleapis.com/%v/%v", bucket, imageNameGS)
	_, pending, err := api.CreateImage(&gcloud.ImageSpec{
		Name:        imageNameGCE,
		SourceImage: storageSrc,
	}, createImageForce)
	if err == nil {
		err = pending.Wait()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Creating GCE image failed: %v\n", err)
		os.Exit(1)
	}
}
