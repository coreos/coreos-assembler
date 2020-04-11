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

package gcloud

import (
	"fmt"
	"strings"

	"golang.org/x/net/context"
	"google.golang.org/api/compute/v0.alpha"
)

type DeprecationState string

const (
	DeprecationStateActive     DeprecationState = "ACTIVE"
	DeprecationStateDeprecated DeprecationState = "DEPRECATED"
	DeprecationStateObsolete   DeprecationState = "OBSOLETE"
	DeprecationStateDeleted    DeprecationState = "DELETED"
)

type ImageSpec struct {
	SourceImage string
	Family      string
	Name        string
	Description string
	Licenses    []string // short names
}

// CreateImage creates an image on GCE and returns operation details and
// a Pending. If overwrite is true, an existing image will be overwritten
// if it exists.
func (a *API) CreateImage(spec *ImageSpec, overwrite bool) (*compute.Operation, *Pending, error) {
	licenses := make([]string, len(spec.Licenses))
	for i, l := range spec.Licenses {
		license, err := a.compute.Licenses.Get(a.options.Project, l).Do()
		if err != nil {
			return nil, nil, fmt.Errorf("Invalid GCE license %s: %v", l, err)
		}
		licenses[i] = license.SelfLink
	}

	if overwrite {
		plog.Debugf("Overwriting image %q", spec.Name)
		// delete existing image, ignore error since it might not exist.
		op, err := a.compute.Images.Delete(a.options.Project, spec.Name).Do()

		if op != nil {
			doable := a.compute.GlobalOperations.Get(a.options.Project, op.Name)
			if err := a.NewPending(op.Name, doable).Wait(); err != nil {
				return nil, nil, err
			}
		}

		// don't return error when delete fails because image doesn't exist
		if err != nil && !strings.HasSuffix(err.Error(), "notFound") {
			return nil, nil, fmt.Errorf("deleting image: %v", err)
		}
	}

	features := []*compute.GuestOsFeature{
		// https://cloud.google.com/compute/docs/images/create-delete-deprecate-private-images
		{
			Type: "VIRTIO_SCSI_MULTIQUEUE",
		},
		{
			Type: "UEFI_COMPATIBLE",
		},
	}

	image := &compute.Image{
		Family:          spec.Family,
		Name:            spec.Name,
		Description:     spec.Description,
		Licenses:        licenses,
		GuestOsFeatures: features,
		RawDisk: &compute.ImageRawDisk{
			Source: spec.SourceImage,
		},
	}

	plog.Debugf("Creating image %q from %q", spec.Name, spec.SourceImage)

	op, err := a.compute.Images.Insert(a.options.Project, image).Do()
	if err != nil {
		return nil, nil, err
	}

	doable := a.compute.GlobalOperations.Get(a.options.Project, op.Name)
	return op, a.NewPending(op.Name, doable), nil
}

func (a *API) ListImages(ctx context.Context, prefix string) ([]*compute.Image, error) {
	var images []*compute.Image
	listReq := a.compute.Images.List(a.options.Project)
	if prefix != "" {
		listReq.Filter(fmt.Sprintf("name eq ^%s.*", prefix))
	}
	err := listReq.Pages(ctx, func(i *compute.ImageList) error {
		images = append(images, i.Items...)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("Listing GCE images failed: %v", err)
	}
	return images, nil
}

func (a *API) GetPendingForImage(image *compute.Image) (*Pending, error) {
	op := a.compute.GlobalOperations.List(a.options.Project)
	op.Filter(fmt.Sprintf("(targetId eq %v) (operationType eq insert)", image.Id))
	pendingOps, err := op.Do()
	if err != nil {
		return nil, fmt.Errorf("Couldn't list pending operations on %q: %v", image.Name, err)
	}
	if len(pendingOps.Items) != 1 {
		return nil, fmt.Errorf("Found %d != 1 insert operations on %q", len(pendingOps.Items), image.Name)
	}
	pendingOp := pendingOps.Items[0]
	doable := a.compute.GlobalOperations.Get(a.options.Project, pendingOp.Name)
	return a.NewPending(pendingOp.Name, doable), nil
}

func (a *API) DeprecateImage(name string, state DeprecationState, replacement string) (*Pending, error) {
	req := a.compute.Images.Deprecate(a.options.Project, name, &compute.DeprecationStatus{
		State:       string(state),
		Replacement: replacement,
	})
	op, err := req.Do()
	if err != nil {
		return nil, fmt.Errorf("Deprecating %s failed: %v", name, err)
	}
	opReq := a.compute.GlobalOperations.Get(a.options.Project, op.Name)
	return a.NewPending(op.Name, opReq), nil
}

func (a *API) DeleteImage(name string) (*Pending, error) {
	op, err := a.compute.Images.Delete(a.options.Project, name).Do()
	if err != nil {
		return nil, fmt.Errorf("Deleting %s failed: %v", name, err)
	}
	opReq := a.compute.GlobalOperations.Get(a.options.Project, op.Name)
	return a.NewPending(op.Name, opReq), nil
}
