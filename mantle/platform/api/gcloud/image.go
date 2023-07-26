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
	"runtime"
	"strings"

	"golang.org/x/net/context"
	"google.golang.org/api/compute/v1"
)

type DeprecationState string

const (
	DeprecationStateActive     DeprecationState = "ACTIVE"
	DeprecationStateDeprecated DeprecationState = "DEPRECATED"
	DeprecationStateObsolete   DeprecationState = "OBSOLETE"
	DeprecationStateDeleted    DeprecationState = "DELETED"
)

type ImageSpec struct {
	Architecture string
	SourceImage  string
	Family       string
	Name         string
	Description  string
	Licenses     []string // short names
}

const endpointPrefix = "https://www.googleapis.com/compute/v1/"

// Given a string representing an image return the full API
// endpoint for the image. For example:
// https://www.googleapis.com/compute/v1/projects/fedora-coreos-cloud/global/images/fedora-coreos-31-20200420-3-0-gcp-x86-64
func getImageAPIEndpoint(image, project string) (string, error) {
	// If the input is already a full API endpoint then just return it
	if strings.HasPrefix(image, endpointPrefix) {
		return image, nil
	}
	// Accept a name beginning with "projects/" to specify a different
	// project from the instance.
	if strings.HasPrefix(image, "projects/") {
		return endpointPrefix + image, nil
	}
	// Also accept a short name (no '/') build API endpoint using
	// instance project (opts.Project).
	if !strings.Contains(image, "/") {
		return fmt.Sprintf(
			"%sprojects/%s/global/images/%s",
			endpointPrefix, project, image), nil
	}
	return "", fmt.Errorf("GCP Image argument must be the full api endpoint," +
		" begin with 'projects/', or use the short name")
}

// CreateImage creates an image on GCP and returns operation details and
// a Pending. If overwrite is true, an existing image will be overwritten
// if it exists.
func (a *API) CreateImage(spec *ImageSpec, overwrite bool) (*compute.Operation, *Pending, error) {
	licenses := make([]string, len(spec.Licenses))
	for i, l := range spec.Licenses {
		// If the license is already in URI format then use that
		if strings.HasPrefix(l, "https://") {
			licenses[i] = l
		} else {
			// If not in URI format then query GCP for that info
			license, err := a.compute.Licenses.Get(a.options.Project, l).Do()
			if err != nil {
				return nil, nil, fmt.Errorf("Invalid GCP license %s: %v", l, err)
			}
			licenses[i] = license.SelfLink
		}
	}

	if spec.Architecture == "" {
		spec.Architecture = runtime.GOARCH
	}
	switch spec.Architecture {
	case "amd64", "x86_64":
		spec.Architecture = "X86_64"
	case "arm64", "aarch64":
		spec.Architecture = "ARM64"
	default:
		return nil, nil, fmt.Errorf("unsupported gcp architecture %q", spec.Architecture)
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
		// RHEL supports this since 8.4; TODO share logic here with
		// https://github.com/osbuild/osbuild-composer/blob/c6570f6c94149b47f2f8e2f82d7467d6b96755bb/internal/cloud/gcp/compute.go#L16
		{
			Type: "SEV_CAPABLE",
		},
		{
			Type: "GVNIC",
		},
		{
			Type: "UEFI_COMPATIBLE",
		},
		// https://cloud.google.com/blog/products/identity-security/rsa-snp-vm-more-confidential
		{
			Type: "SEV_SNP_CAPABLE",
		},
	}

	image := &compute.Image{
		Architecture:    spec.Architecture,
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

func (a *API) ListImages(ctx context.Context, prefix string, family string) ([]*compute.Image, error) {
	var images []*compute.Image
	listReq := a.compute.Images.List(a.options.Project)
	if prefix != "" {
		listReq.Filter(fmt.Sprintf("name eq ^%s.*", prefix))
	}
	if family != "" {
		listReq.Filter(fmt.Sprintf("family eq ^%s$", family))
	}
	err := listReq.Pages(ctx, func(i *compute.ImageList) error {
		images = append(images, i.Items...)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("Listing GCP images failed: %v", err)
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
	var err error

	if replacement != "" {
		replacement, err = getImageAPIEndpoint(replacement, a.options.Project)
		if err != nil {
			return nil, err
		}
	}

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

func (a *API) UpdateImage(name string, family string, description string) (*Pending, error) {

	// The docs say the following fields can be modified:
	//      family, description, deprecation status
	// but deprecation status did not seem to work when tested.
	image := &compute.Image{
		Family:      family,
		Description: description,
	}

	req := a.compute.Images.Patch(a.options.Project, name, image)
	op, err := req.Do()
	if err != nil {
		return nil, fmt.Errorf("Updating %s failed: %v", name, err)
	}
	opReq := a.compute.GlobalOperations.Get(a.options.Project, op.Name)
	return a.NewPending(op.Name, opReq), nil
}

// https://cloud.google.com/compute/docs/images/managing-access-custom-images#share-images-publicly
func (a *API) SetImagePublic(name string) error {
	// The IAM policy binding to allow all authenticated users to
	// use an image
	publicbinding := &compute.Binding{
		Members: []string{"allAuthenticatedUsers"},
		Role:    "roles/compute.imageUser",
	}

	// Get the current policy for the image
	policy, err := a.compute.Images.GetIamPolicy(a.options.Project, name).Do()
	if err != nil {
		return fmt.Errorf("Getting image %s IAM policy failed: %v", name, err)
	}

	// Add entries to the policy to make the image public.
	policy.Bindings = append(policy.Bindings, publicbinding)

	// Make the call to make it public. If the image is already
	// public for whatever reason doing this has no effect.
	globalsetpolicyrequest := &compute.GlobalSetPolicyRequest{
		Policy: policy,
	}
	_, err = a.compute.Images.SetIamPolicy(
		a.options.Project, name, globalsetpolicyrequest).Do()
	if err != nil {
		return fmt.Errorf("Setting image %s IAM policy failed: %v", name, err)
	}
	return nil
}
