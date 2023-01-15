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
	"google.golang.org/api/option"
	"net/http"
	"time"

	"github.com/coreos/pkg/capnslog"
	"google.golang.org/api/compute/v1"

	"github.com/coreos/coreos-assembler/mantle/auth"
	"github.com/coreos/coreos-assembler/mantle/platform"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/coreos-assembler/mantle", "platform/api/gcloud")
)

type Options struct {
	Image       string
	Project     string
	Zone        string
	MachineType string
	DiskType    string
	Network     string
	ServiceAcct string
	JSONKeyFile string
	ServiceAuth bool
	*platform.Options
}

type API struct {
	client  *http.Client
	compute *compute.Service
	options *Options
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
	}

	return api, nil
}

func (a *API) Client() *http.Client {
	return a.client
}

func (a *API) GC(gracePeriod time.Duration) error {
	return a.gcInstances(gracePeriod)
}
