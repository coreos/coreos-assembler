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
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/net/context"
	"google.golang.org/api/compute/v1"
	gs "google.golang.org/api/storage/v1"

	"github.com/coreos/mantle/auth"
	"github.com/coreos/mantle/storage"
	"github.com/coreos/mantle/storage/index"
)

var (
	releaseBatch  bool
	releaseDryRun bool
	cmdRelease    = &cobra.Command{
		Use:   "release [options]",
		Short: "Publish a new CoreOS release.",
		Run:   runRelease,
		Long: `Publish a new CoreOS release.

TODO`,
	}
)

func init() {
	cmdRelease.Flags().BoolVarP(&releaseDryRun, "dry-run", "n", false,
		"perform a trial run, do not make changes")
	AddSpecFlags(cmdRelease.Flags())
	root.AddCommand(cmdRelease)
}

func runRelease(cmd *cobra.Command, args []string) {
	if len(args) > 0 {
		plog.Fatal("No args accepted")
	}

	spec := ChannelSpec()
	ctx := context.Background()
	client, err := auth.GoogleClient()
	if err != nil {
		plog.Fatalf("Authentication failed: %v", err)
	}

	src, err := storage.NewBucket(client, spec.SourceURL())
	if err != nil {
		plog.Fatal(err)
	}
	src.WriteDryRun(releaseDryRun)

	if err := src.Fetch(ctx); err != nil {
		plog.Fatal(err)
	}

	// Sanity check!
	if vertxt := src.Object(src.Prefix() + "version.txt"); vertxt == nil {
		verurl := src.URL().String() + "version.txt"
		plog.Fatalf("File not found: %s", verurl)
	}

	// Register GCE image if needed.
	doGCE(ctx, client, src, &spec)

	for _, dSpec := range spec.Destinations {
		dst, err := storage.NewBucket(client, dSpec.BaseURL)
		if err != nil {
			plog.Fatal(err)
		}
		dst.WriteDryRun(releaseDryRun)

		// Fetch parent directories non-recursively to re-index it later.
		for _, prefix := range dSpec.ParentPrefixes() {
			if err := dst.FetchPrefix(ctx, prefix, false); err != nil {
				plog.Fatal(err)
			}
		}

		// Fetch and sync each destination directory.
		for _, prefix := range dSpec.FinalPrefixes() {
			if err := dst.FetchPrefix(ctx, prefix, true); err != nil {
				plog.Fatal(err)
			}

			sync := index.NewSyncIndexJob(src, dst)
			sync.DestinationPrefix(prefix)
			sync.DirectoryHTML(dSpec.DirectoryHTML)
			sync.IndexHTML(dSpec.IndexHTML)
			sync.Delete(true)
			if dSpec.Title != "" {
				sync.Name(dSpec.Title)
			}
			if err := sync.Do(ctx); err != nil {
				plog.Fatal(err)
			}
		}

		// Now refresh the parent directory indexes.
		for _, prefix := range dSpec.ParentPrefixes() {
			parent := index.NewIndexJob(dst)
			parent.Prefix(prefix)
			parent.DirectoryHTML(dSpec.DirectoryHTML)
			parent.IndexHTML(dSpec.IndexHTML)
			parent.Recursive(false)
			parent.Delete(true)
			if dSpec.Title != "" {
				parent.Name(dSpec.Title)
			}
			if err := parent.Do(ctx); err != nil {
				plog.Fatal(err)
			}
		}
	}
}

func sanitizeVersion() string {
	v := strings.Replace(specVersion, ".", "-", -1)
	return strings.Replace(v, "+", "-", -1)
}

func doGCE(ctx context.Context, client *http.Client, src *storage.Bucket, spec *channelSpec) {
	if spec.GCE.Project == "" || spec.GCE.Image == "" {
		plog.Notice("GCE image creation disabled.")
		return
	}

	api, err := compute.New(client)
	if err != nil {
		plog.Fatalf("GCE client failed: %v", err)
	}

	publishImage := func(image string) {
		if spec.GCE.Publish == "" {
			plog.Notice("GCE image name publishing disabled.")
			return
		}
		obj := gs.Object{
			Name:        src.Prefix() + spec.GCE.Publish,
			ContentType: "text/plain",
		}
		media := strings.NewReader(
			fmt.Sprintf("projects/%s/global/images/%s\n",
				spec.GCE.Project, image))
		if err := src.Upload(ctx, &obj, media); err != nil {
			plog.Fatal(err)
		}
	}

	nameVer := fmt.Sprintf("%s-%s-v", spec.GCE.Family, sanitizeVersion())
	date := time.Now().UTC()
	name := nameVer + date.Format("20060102")
	desc := fmt.Sprintf("%s, %s, %s published on %s", spec.GCE.Description,
		specVersion, specBoard, date.Format("2006-01-02"))

	var images []*compute.Image
	listReq := api.Images.List(spec.GCE.Project)
	listReq.Filter(fmt.Sprintf("name eq ^%s-.*", spec.GCE.Family))
	if err := listReq.Pages(ctx, func(i *compute.ImageList) error {
		images = append(images, i.Items...)
		return nil
	}); err != nil {
		plog.Fatalf("Listing GCE images failed: %v", err)
	}

	var conflicting []string
	for _, image := range images {
		if strings.HasPrefix(image.Name, nameVer) {
			conflicting = append(conflicting, image.Name)
		}
	}

	// Check for any with the same version but possibly different dates.
	if len(conflicting) > 1 {
		plog.Fatalf("Duplicate GCE images found: %v", conflicting)
	} else if len(conflicting) == 1 {
		plog.Noticef("GCE image already exists: %s", conflicting[0])
		publishImage(conflicting[0])
		return
	}

	if spec.GCE.Limit > 0 && len(images) > spec.GCE.Limit {
		plog.Noticef("Pruning %d GCE images.", len(images)-spec.GCE.Limit)
		plog.Notice("NOPE! JUST KIDDING, TODO")
	}

	obj := src.Object(src.Prefix() + spec.GCE.Image)
	if obj == nil {
		plog.Fatalf("GCE image not found %s%s", src.URL(), spec.GCE.Image)
	}

	licenses := make([]string, len(spec.GCE.Licenses))
	for i, l := range spec.GCE.Licenses {
		req := api.Licenses.Get(spec.GCE.Project, l)
		req.Context(ctx)
		license, err := req.Do()
		if err != nil {
			plog.Fatalf("Invalid GCE license %s: %v", l, err)
		}
		licenses[i] = license.SelfLink
	}

	image := &compute.Image{
		Family:           spec.GCE.Family,
		Name:             name,
		Description:      desc,
		Licenses:         licenses,
		ArchiveSizeBytes: int64(obj.Size),
		RawDisk: &compute.ImageRawDisk{
			Source: obj.MediaLink,
			// TODO: include sha1
		},
	}

	if releaseDryRun {
		plog.Noticef("Would create GCE image %s", name)
		return
	}

	plog.Noticef("Creating GCE image %s", image.Name)
	insReq := api.Images.Insert(spec.GCE.Project, image)
	insReq.Context(ctx)
	op, err := insReq.Do()
	if err != nil {
		plog.Fatalf("GCE image creation failed: %v", err)
	}

	plog.Infof("Waiting for image creation to finish...")
	failures := 0
	for op == nil || op.Status != "DONE" {
		if op != nil {
			status := strings.ToLower(op.Status)
			if op.Progress != 0 {
				plog.Infof("Image creation is %s: %s % 2d%%",
					status, op.StatusMessage, op.Progress)
			} else {
				plog.Infof("Image creation is %s. %s", status, op.StatusMessage)
			}
		}

		time.Sleep(3 * time.Second)
		opReq := api.GlobalOperations.Get(spec.GCE.Project, op.Name)
		opReq.Context(ctx)
		op, err = opReq.Do()
		if err != nil {
			plog.Errorf("Fetching status failed: %v", err)
			failures++
			if failures > 5 {
				plog.Fatalf("Giving up after %d failures.", failures)
			}
		}
	}

	if op.Error != nil {
		plog.Fatalf("Image creation failed: %+v", op.Error.Errors)
	}

	plog.Info("Success!")

	publishImage(name)

	var pending map[string]*compute.Operation
	addPending := func(op *compute.Operation) {
		if op.Status == "DONE" {
			delete(pending, op.Name)
			if op.Error != nil {
				plog.Fatalf("Operation failed: %+v", op.Error.Errors)
			}
			return
		}
		pending[op.Name] = op
		status := strings.ToLower(op.Status)
		if op.Progress != 0 {
			plog.Infof("Operation is %s: %s % 2d%%",
				status, op.StatusMessage, op.Progress)
		} else {
			plog.Infof("Operation is %s. %s", status, op.StatusMessage)
		}
	}

	for _, old := range images {
		if old.Deprecated != nil && old.Deprecated.State != "" {
			continue
		}
		plog.Noticef("Deprecating old image %s", old.Name)
		status := &compute.DeprecationStatus{
			State:       "DEPRECATED",
			Replacement: op.TargetLink,
		}
		req := api.Images.Deprecate(spec.GCE.Project, old.Name, status)
		req.Context(ctx)
		op, err := req.Do()
		if err != nil {
			plog.Fatalf("Deprecating %s failed: %v", old.Name, err)
		}
		addPending(op)
	}

	updatePending := func(ops *compute.OperationList) error {
		for _, op := range ops.Items {
			if _, ok := pending[op.Name]; ok {
				addPending(op)
			}
		}
		return nil
	}

	failures = 0
	for len(pending) > 0 {
		plog.Infof("Waiting on %s operations.", len(pending))
		time.Sleep(1 * time.Second)
		opReq := api.GlobalOperations.List(spec.GCE.Project)
		if err := opReq.Pages(ctx, updatePending); err != nil {
			plog.Errorf("Fetching status failed: %v", err)
			failures++
			if failures > 5 {
				plog.Fatalf("Giving up after %d failures.", failures)
			}
		}
	}
}
