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
	// Numeric image ID, {alpha, beta, stable}, or user image name
	Image string
	// Whether to allocate an IPv6 address to the droplet.
	DisableIPv6 bool
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

	ctx := context.TODO()
	client := godo.NewClient(oauth2.NewClient(ctx, &tokenSource{opts.AccessToken}))

	a := &API{
		c:    client,
		opts: opts,
	}

	var err error
	a.image, err = a.resolveImage(ctx, opts.Image)
	if err != nil {
		return nil, err
	}

	return a, nil
}

func (a *API) resolveImage(ctx context.Context, imageSpec string) (godo.DropletCreateImage, error) {
	// try numeric image ID first
	imageID, err := strconv.Atoi(imageSpec)
	if err == nil {
		return godo.DropletCreateImage{ID: imageID}, nil
	}

	// handle magic values
	switch imageSpec {
	case "":
		// pick the most conservative default
		imageSpec = "stable"
		fallthrough
	case "alpha", "beta", "stable":
		return godo.DropletCreateImage{Slug: "coreos-" + imageSpec}, nil
	}

	// resolve to user image ID
	image, err := a.GetUserImage(ctx, imageSpec, true)
	if err == nil {
		// Custom images don't support IPv6
		if image.Type == "custom" {
			a.opts.DisableIPv6 = true
		}
		return godo.DropletCreateImage{ID: image.ID}, nil
	}

	return godo.DropletCreateImage{}, fmt.Errorf("couldn't resolve image %q in %v", imageSpec, a.opts.Region)
}

func (a *API) PreflightCheck(ctx context.Context) error {
	_, _, err := a.c.Account.Get(ctx)
	if err != nil {
		return fmt.Errorf("querying account: %v", err)
	}
	return nil
}

func (a *API) CreateDroplet(ctx context.Context, name string, sshKeyID int, userdata string) (*godo.Droplet, error) {
	var droplet *godo.Droplet
	var err error
	// DO frequently gives us 422 errors saying "Please try again". Retry every 10 seconds
	// for up to 5 min
	err = util.RetryConditional(5*6, 10*time.Second, shouldRetry, func() error {
		droplet, _, err = a.c.Droplets.Create(ctx, &godo.DropletCreateRequest{
			Name:              name,
			Region:            a.opts.Region,
			Size:              a.opts.Size,
			Image:             a.image,
			SSHKeys:           []godo.DropletCreateSSHKey{{ID: sshKeyID}},
			IPv6:              !a.opts.DisableIPv6,
			PrivateNetworking: true,
			UserData:          userdata,
			Tags:              []string{"mantle"},
		})
		if err != nil {
			plog.Errorf("Error creating droplet: %v. Retrying...", err)
		}
		return err
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
		if errDelete := a.DeleteDroplet(ctx, dropletID); errDelete != nil {
			return nil, fmt.Errorf("deleting droplet failed: %v after running droplet failed: %v", errDelete, err)
		}
		return nil, fmt.Errorf("waiting for droplet to run: %v", err)
	}

	return droplet, nil
}

func (a *API) listDropletsWithTag(ctx context.Context, tag string) ([]godo.Droplet, error) {
	page := godo.ListOptions{
		Page:    1,
		PerPage: 200,
	}
	var ret []godo.Droplet
	for {
		droplets, _, err := a.c.Droplets.ListByTag(ctx, tag, &page)
		if err != nil {
			return nil, err
		}
		ret = append(ret, droplets...)
		if len(droplets) < page.PerPage {
			return ret, nil
		}
		page.Page += 1
	}
}

func (a *API) GetDroplet(ctx context.Context, dropletID int) (*godo.Droplet, error) {
	droplet, _, err := a.c.Droplets.Get(ctx, dropletID)
	if err != nil {
		return nil, err
	}
	return droplet, nil
}

// SnapshotDroplet creates a snapshot of a droplet and waits until complete.
// The Snapshot API doesn't return the snapshot ID, so we don't either.
func (a *API) SnapshotDroplet(ctx context.Context, dropletID int, name string) error {
	action, _, err := a.c.DropletActions.Snapshot(ctx, dropletID, name)
	if err != nil {
		return err
	}
	actionID := action.ID

	err = util.WaitUntilReady(30*time.Minute, 15*time.Second, func() (bool, error) {
		action, _, err := a.c.Actions.Get(ctx, actionID)
		if err != nil {
			return false, err
		}
		switch action.Status {
		case "in-progress":
			return false, nil
		case "completed":
			return true, nil
		default:
			return false, fmt.Errorf("snapshot failed")
		}
	})
	if err != nil {
		return err
	}

	return nil
}

func (a *API) DeleteDroplet(ctx context.Context, dropletID int) error {
	_, err := a.c.Droplets.Delete(ctx, dropletID)
	if err != nil {
		return fmt.Errorf("deleting droplet %d: %v", dropletID, err)
	}
	return nil
}

func (a *API) CreateCustomImage(ctx context.Context, name string, url string) (*godo.Image, error) {
	var image *godo.Image
	var err error
	image, _, err = a.c.Images.Create(ctx, &godo.CustomImageCreateRequest{
		Name:         name,
		Url:          url,
		Region:       a.opts.Region,
		Distribution: "Fedora",
		Tags:         []string{"mantle"},
	})
	if err != nil {
		return nil, fmt.Errorf("couldn't create image: %v", err)
	}
	imageID := image.ID

	err = util.WaitUntilReady(10*time.Minute, 10*time.Second, func() (bool, error) {
		var err error
		// update image in closure
		image, _, err = a.c.Images.GetByID(ctx, imageID)
		if err != nil {
			return false, err
		}
		return image.Status == "available", nil
	})
	if err != nil {
		if errDelete := a.DeleteImage(ctx, imageID); errDelete != nil {
			return nil, fmt.Errorf("deleting image failed: %v after creating image failed: %v", errDelete, err)
		}
		return nil, fmt.Errorf("waiting for image creation: %v", err)
	}

	return image, nil
}

func (a *API) GetUserImage(ctx context.Context, imageName string, inRegion bool) (*godo.Image, error) {
	var ret *godo.Image
	var regionMessage string
	if inRegion {
		regionMessage = fmt.Sprintf(" in %v", a.opts.Region)
	}
	page := godo.ListOptions{
		Page:    1,
		PerPage: 200,
	}
	for {
		images, _, err := a.c.Images.ListUser(ctx, &page)
		if err != nil {
			return nil, err
		}
		for _, image := range images {
			image := image
			if image.Name != imageName {
				continue
			}
			for _, region := range image.Regions {
				if inRegion && region != a.opts.Region {
					continue
				}
				if ret != nil {
					return nil, fmt.Errorf("found multiple images named %q%s", imageName, regionMessage)
				}
				ret = &image
				break
			}
		}
		if len(images) < page.PerPage {
			break
		}
		page.Page += 1
	}

	if ret == nil {
		return nil, fmt.Errorf("couldn't find image %q%s", imageName, regionMessage)
	}
	return ret, nil
}

func (a *API) DeleteImage(ctx context.Context, imageID int) error {
	_, err := a.c.Images.Delete(ctx, imageID)
	if err != nil {
		return fmt.Errorf("deleting image %d: %v", imageID, err)
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

func (a *API) ListKeys(ctx context.Context) ([]godo.Key, error) {
	page := godo.ListOptions{
		Page:    1,
		PerPage: 200,
	}
	var ret []godo.Key
	for {
		keys, _, err := a.c.Keys.List(ctx, &page)
		if err != nil {
			return nil, err
		}
		ret = append(ret, keys...)
		if len(keys) < page.PerPage {
			return ret, nil
		}
		page.Page += 1
	}
}

func (a *API) GC(ctx context.Context, gracePeriod time.Duration) error {
	threshold := time.Now().Add(-gracePeriod)

	droplets, err := a.listDropletsWithTag(ctx, "mantle")
	if err != nil {
		return fmt.Errorf("listing droplets: %v", err)
	}
	for _, droplet := range droplets {
		if droplet.Status == "archive" {
			continue
		}

		created, err := time.Parse(time.RFC3339, droplet.Created)
		if err != nil {
			return fmt.Errorf("couldn't parse %q: %v", droplet.Created, err)
		}
		if created.After(threshold) {
			continue
		}

		if err := a.DeleteDroplet(ctx, droplet.ID); err != nil {
			return fmt.Errorf("couldn't delete droplet %d: %v", droplet.ID, err)
		}
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

// shouldRetry returns if the error is from DigitalOcean and we should
// retry the request which generated it
func shouldRetry(err error) bool {
	errResp, ok := err.(*godo.ErrorResponse)
	if !ok {
		return false
	}
	status := errResp.Response.StatusCode
	return status == 422 || status >= 500
}
