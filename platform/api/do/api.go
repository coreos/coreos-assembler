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

package do

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/coreos/pkg/capnslog"
	"github.com/digitalocean/godo"
	"golang.org/x/oauth2"

	"github.com/coreos/mantle/auth"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/util"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "platform/api/do")
)

type Options struct {
	*platform.Options

	// Config file. Defaults to $HOME/.config/digitalocean.json.
	ConfigPath string
	// Profile name
	Profile string
	// Personal access token (overrides config profile)
	AccessToken string

	// Region slug (e.g. "sfo2")
	Region string
	// Droplet size slug (e.g. "512mb")
	Size string
	// Numeric image ID or {alpha, beta, stable}
	Image string
}

type API struct {
	c     *godo.Client
	opts  *Options
	image godo.DropletCreateImage
}

func New(opts *Options) (*API, error) {
	if opts.AccessToken == "" {
		profiles, err := auth.ReadDOConfig(opts.ConfigPath)
		if err != nil {
			return nil, fmt.Errorf("couldn't read DigitalOcean config: %v", err)
		}

		if opts.Profile == "" {
			opts.Profile = "default"
		}
		profile, ok := profiles[opts.Profile]
		if !ok {
			return nil, fmt.Errorf("no such profile %q", opts.Profile)
		}
		if opts.AccessToken == "" {
			opts.AccessToken = profile.AccessToken
		}
	}

	client := godo.NewClient(oauth2.NewClient(context.TODO(), &tokenSource{opts.AccessToken}))

	image, err := resolveImage(opts.Image)
	if err != nil {
		return nil, err
	}

	return &API{
		c:     client,
		opts:  opts,
		image: image,
	}, nil
}

func resolveImage(imageSpec string) (godo.DropletCreateImage, error) {
	imageID, err := strconv.Atoi(imageSpec)
	if err == nil {
		return godo.DropletCreateImage{ID: imageID}, nil
	}

	switch imageSpec {
	case "alpha", "beta", "stable":
		return godo.DropletCreateImage{Slug: "coreos-" + imageSpec}, nil
	default:
		return godo.DropletCreateImage{}, fmt.Errorf("couldn't resolve image %q", imageSpec)
	}
}

func (a *API) CreateDroplet(ctx context.Context, name string, sshKeyID int, userdata string) (*godo.Droplet, error) {
	var keys []godo.DropletCreateSSHKey
	if sshKeyID != 0 {
		keys = append(keys, godo.DropletCreateSSHKey{ID: sshKeyID})
	}

	droplet, _, err := a.c.Droplets.Create(ctx, &godo.DropletCreateRequest{
		Name:              name,
		Region:            a.opts.Region,
		Size:              a.opts.Size,
		Image:             a.image,
		SSHKeys:           keys,
		IPv6:              true,
		PrivateNetworking: true,
		UserData:          userdata,
		Tags:              []string{"mantle"},
	})
	if err != nil {
		return nil, fmt.Errorf("couldn't create droplet: %v", err)
	}
	dropletID := droplet.ID

	err = util.WaitUntilReady(5*time.Minute, 10*time.Second, func() (bool, error) {
		var err error
		// update droplet in closure
		droplet, _, err = a.c.Droplets.Get(ctx, dropletID)
		if err != nil {
			return false, err
		}
		return droplet.Status == "active", nil
	})
	if err != nil {
		a.DeleteDroplet(ctx, dropletID)
		return nil, fmt.Errorf("waiting for droplet to run: %v", err)
	}

	return droplet, nil
}

func (a *API) DeleteDroplet(ctx context.Context, dropletID int) error {
	_, err := a.c.Droplets.Delete(ctx, dropletID)
	if err != nil {
		return fmt.Errorf("deleting droplet %d: %v", dropletID, err)
	}
	return nil
}

func (a *API) AddKey(ctx context.Context, name, key string) (int, error) {
	sshKey, _, err := a.c.Keys.Create(ctx, &godo.KeyCreateRequest{
		Name:      name,
		PublicKey: key,
	})
	if err != nil {
		return 0, fmt.Errorf("couldn't create SSH key: %v", err)
	}
	return sshKey.ID, nil
}

func (a *API) DeleteKey(ctx context.Context, keyID int) error {
	_, err := a.c.Keys.DeleteByID(ctx, keyID)
	if err != nil {
		return fmt.Errorf("couldn't delete SSH key: %v", err)
	}
	return nil
}

type tokenSource struct {
	token string
}

func (t *tokenSource) Token() (*oauth2.Token, error) {
	return &oauth2.Token{
		AccessToken: t.token,
	}, nil
}
