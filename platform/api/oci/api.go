// Copyright 2017 CoreOS, Inc.
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

package oci

import (
	"fmt"
	"time"

	"github.com/oracle/bmcs-go-sdk"

	"github.com/coreos/mantle/auth"
	"github.com/coreos/mantle/platform"
)

type Options struct {
	*platform.Options

	ConfigPath string
	Profile    string

	TenancyID          string
	UserID             string
	Fingerprint        string
	KeyFile            string
	PrivateKeyPassword string
	Region             string

	CompartmentID string
	Image         string
	Shape         string
}

type Machine struct {
	Name             string
	ID               string
	PublicIPAddress  string
	PrivateIPAddress string
}

type API struct {
	client *baremetal.Client
	opts   *Options
}

func New(opts *Options) (*API, error) {
	profiles, err := auth.ReadOCIConfig(opts.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("couldn't read OCI profile: %v", err)
	}

	if opts.Profile == "" {
		opts.Profile = "default"
	}

	profile, ok := profiles[opts.Profile]
	if !ok {
		return nil, fmt.Errorf("no such profile %q", opts.Profile)
	}

	if opts.TenancyID == "" {
		opts.TenancyID = profile.TenancyID
	}
	if opts.UserID == "" {
		opts.UserID = profile.UserID
	}
	if opts.Fingerprint == "" {
		opts.Fingerprint = profile.Fingerprint
	}
	if opts.KeyFile == "" {
		opts.KeyFile = profile.KeyFile
	}
	if opts.PrivateKeyPassword == "" {
		opts.PrivateKeyPassword = profile.PrivateKeyPassword
	}
	if opts.Region == "" {
		opts.Region = profile.Region
	}
	if opts.CompartmentID == "" {
		opts.CompartmentID = profile.CompartmentID
	}

	extraOpts := []baremetal.NewClientOptionsFunc{}
	extraOpts = append(extraOpts, baremetal.PrivateKeyFilePath(opts.KeyFile))

	if opts.Region != "" {
		extraOpts = append(extraOpts, baremetal.Region(opts.Region))
	}

	if opts.PrivateKeyPassword != "" {
		extraOpts = append(extraOpts, baremetal.PrivateKeyPassword(opts.PrivateKeyPassword))
	}

	client, err := baremetal.NewClient(opts.UserID, opts.TenancyID, opts.Fingerprint, extraOpts...)
	if err != nil {
		return nil, err
	}

	return &API{
		client: client,
		opts:   opts,
	}, nil
}

func (a *API) GC(gracePeriod time.Duration) error {
	return a.gcInstances(gracePeriod)
}

func boolToPtr(b bool) *bool {
	return &b
}
