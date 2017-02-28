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

	"google.golang.org/api/compute/v1"
)

// CreateImage creates an image on GCE and and wait for completion. If
// overwrite is true, an existing image will be overwritten if it exists.
func (a *API) CreateImage(name, source string, overwrite bool) error {
	if overwrite {
		plog.Debugf("Overwriting image %q", name)
		// delete existing image, ignore error since it might not exist.
		op, err := a.compute.Images.Delete(a.options.Project, name).Do()

		if op != nil {
			doable := a.compute.GlobalOperations.Get(a.options.Project, op.Name)
			if err := a.waitop(op.Name, doable); err != nil {
				return err
			}
		}

		// don't return error when delete fails because image doesn't exist
		if err != nil && !strings.HasSuffix(err.Error(), "notFound") {
			return fmt.Errorf("deleting image: %v", err)
		}
	}

	image := &compute.Image{
		Name: name,
		RawDisk: &compute.ImageRawDisk{
			Source: source,
		},
	}

	plog.Debugf("Creating image %q from %q", name, source)

	op, err := a.compute.Images.Insert(a.options.Project, image).Do()
	if err != nil {
		return err
	}

	doable := a.compute.GlobalOperations.Get(a.options.Project, op.Name)
	if err := a.waitop(op.Name, doable); err != nil {
		return err
	}

	plog.Debugf("Created image %q from %q", name, source)

	return nil
}

func (a *API) ListImages(prefix string) ([]string, error) {
	var images []string

	list, err := a.compute.Images.List(a.options.Project).Do()
	if err != nil {
		return nil, err
	}

	for _, image := range list.Items {
		if !strings.HasPrefix(image.Name, prefix) {
			continue
		}

		images = append(images, image.Name)
	}

	return images, nil
}
