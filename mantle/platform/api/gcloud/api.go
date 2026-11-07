// Copyright 2015 CoreOS, Inc.
// Copyright 2015 The Go Authors.
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
	"net/http"
	"regexp"
	"time"

	"google.golang.org/api/option"

	"github.com/coreos/pkg/capnslog"
	"google.golang.org/api/compute/v1"

	"github.com/coreos/coreos-assembler/mantle/auth"
	"github.com/coreos/coreos-assembler/mantle/platform"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/coreos-assembler/mantle", "platform/api/gcloud")
)

type Options struct {
	Image            string
	Project          string
	PreferredZone    string
	MachineType      string
	DiskType         string
	Network          string
	ServiceAcct      string
	JSONKeyFile      string
	ServiceAuth      bool
	ConfidentialType string
	*platform.Options
}

type API struct {
	client  *http.Client
	compute *compute.Service
	options *Options
	zones   []string
}

// This regex should match all standard (non-AI) zones
// See: https://docs.cloud.google.com/compute/docs/regions-zones
var standardZoneRegexp = regexp.MustCompile(`^([a-z]+-[a-z]+\d+)-[a-z]$`)

// zones are in the form "us-central1-a" and the region would be "us-central1"
// See: https://docs.cloud.google.com/compute/docs/regions-zones
func extractRegionFromZone(zone string) (string, error) {
	matches := standardZoneRegexp.FindStringSubmatch(zone)
	if matches == nil {
		return "", fmt.Errorf("zone %q does not match expected format {region}-{letter}", zone)
	}
	return matches[1], nil
}

func getAvailableZones(computeService *compute.Service, opts *Options) ([]string, error) {
	if opts.MachineType == "" {
		return []string{opts.PreferredZone}, nil
	}

	list, err := computeService.MachineTypes.AggregatedList(opts.Project).
		Filter("name=" + opts.MachineType).Do()

	if err != nil {
		return nil, err
	}

	targetRegion, err := extractRegionFromZone(opts.PreferredZone)
	if err != nil {
		return nil, fmt.Errorf("could not extract region from zone %q: %w", opts.PreferredZone, err)
	}

	zones := []string{}
	for _, scopedList := range list.Items {
		// There should be either 1 or 0 MachineTypes
		// 0 if this zone does not have the required machine type
		// 1 if this zone does have the required machine type
		if len(scopedList.MachineTypes) == 0 {
			continue
		}
		if len(scopedList.MachineTypes) > 1 {
			plog.Warningf("Unexpected: got %d machine types for filter name=%s", len(scopedList.MachineTypes), opts.MachineType)
			continue
		}
		zone := scopedList.MachineTypes[0].Zone
		if region, err := extractRegionFromZone(zone); err == nil && region == targetRegion {
			// If the preferred zone can be used, it should be the first zone that we use,
			// so we will make add it to the start of the list, rather than the end.
			if zone == opts.PreferredZone {
				zones = append([]string{zone}, zones...)
			} else {
				zones = append(zones, zone)
			}
		}
	}

	if len(zones) == 0 {
		return zones, fmt.Errorf("no zones in region %s for machine type %s were found", targetRegion, opts.MachineType)
	}
	return zones, nil
}

func New(opts *Options) (*API, error) {

	var (
		client *http.Client
		err    error
	)

	if opts.Image != "" {
		opts.Image, err = getImageAPIEndpoint(opts.Image, opts.Project)
		if err != nil {
			return nil, err
		}
	}

	if opts.ServiceAuth {
		client = auth.GoogleServiceClient()
	} else {
		client, err = auth.GoogleClientFromKeyFile(opts.JSONKeyFile)
		if err != nil {
			plog.Fatal(err)
			return nil, err
		}
	}

	ctx := context.Background()

	computeService, err := compute.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, err
	}

	zones, err := getAvailableZones(computeService, opts)
	if err != nil {
		plog.Warningf("Failed to discover available zones: %v. Falling back to preferred zone (%s) only.", err, opts.PreferredZone)
		zones = []string{opts.PreferredZone}
	}

	if opts.ServiceAcct == "" {
		proj, err := computeService.Projects.Get(opts.Project).Do()
		if err != nil {
			return nil, err
		}
		opts.ServiceAcct = proj.DefaultServiceAccount
	}

	api := &API{
		client:  client,
		compute: computeService,
		options: opts,
		zones:   zones,
	}

	return api, nil
}

func (a *API) Client() *http.Client {
	return a.client
}

func (a *API) GC(gracePeriod time.Duration) error {
	for _, zone := range a.zones {
		if err := a.gcInstances(gracePeriod, zone); err != nil {
			return err
		}
	}
	return nil
}
