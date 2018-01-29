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
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/coreos/pkg/capnslog"
	"github.com/digitalocean/godo"

	ctplatform "github.com/coreos/container-linux-config-transpiler/config/platform"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/api/do"
	"github.com/coreos/mantle/platform/conf"
	"github.com/coreos/mantle/util"
)

const (
	Platform platform.Name = "do"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "platform/machine/do")
)

type cluster struct {
	*platform.BaseCluster
	api      *do.API
	sshKeyID int
}

func NewCluster(opts *do.Options, rconf *platform.RuntimeConfig) (platform.Cluster, error) {
	api, err := do.New(opts)
	if err != nil {
		return nil, err
	}

	bc, err := platform.NewBaseCluster(opts.BaseName, rconf, Platform, ctplatform.DO)
	if err != nil {
		return nil, err
	}

	var key string
	if !rconf.NoSSHKeyInMetadata {
		keys, err := bc.Keys()
		if err != nil {
			return nil, err
		}
		key = keys[0].String()
	} else {
		// The DO API requires us to provide an SSH key for
		// Container Linux droplets. Provide one that can never
		// authenticate.
		key, err = do.GenerateFakeKey()
		if err != nil {
			return nil, err
		}
	}
	keyID, err := api.AddKey(context.TODO(), bc.Name(), key)
	if err != nil {
		return nil, err
	}

	return &cluster{
		BaseCluster: bc,
		api:         api,
		sshKeyID:    keyID,
	}, nil
}

func (dc *cluster) NewMachine(userdata *conf.UserData) (platform.Machine, error) {
	conf, err := dc.RenderUserData(userdata, map[string]string{
		"$public_ipv4":  "${COREOS_DIGITALOCEAN_IPV4_PUBLIC_0}",
		"$private_ipv4": "${COREOS_DIGITALOCEAN_IPV4_PRIVATE_0}",
	})
	if err != nil {
		return nil, err
	}

	var droplet *godo.Droplet
	// DO frequently gives us 422 errors saying "Please try again"
	if err := util.RetryConditional(6, 10*time.Second, shouldRetry, func() error {
		droplet, err = dc.api.CreateDroplet(context.TODO(), dc.vmname(), dc.sshKeyID, conf.String())
		if err != nil {
			plog.Errorf("Error creating droplet: %v, retrying...", err)
		}
		return err
	}); err != nil {
		return nil, err
	}

	mach := &machine{
		cluster: dc,
		droplet: droplet,
	}
	mach.publicIP, err = droplet.PublicIPv4()
	if mach.publicIP == "" || err != nil {
		mach.Destroy()
		return nil, fmt.Errorf("couldn't get public IP address for droplet: %v", err)
	}
	mach.privateIP, err = droplet.PrivateIPv4()
	if mach.privateIP == "" || err != nil {
		mach.Destroy()
		return nil, fmt.Errorf("couldn't get private IP address for droplet: %v", err)
	}

	dir := filepath.Join(dc.RuntimeConf().OutputDir, mach.ID())
	if err := os.Mkdir(dir, 0777); err != nil {
		mach.Destroy()
		return nil, err
	}

	confPath := filepath.Join(dir, "user-data")
	if err := conf.WriteFile(confPath); err != nil {
		mach.Destroy()
		return nil, err
	}

	if mach.journal, err = platform.NewJournal(dir); err != nil {
		mach.Destroy()
		return nil, err
	}

	if err := platform.StartMachine(mach, mach.journal); err != nil {
		mach.Destroy()
		return nil, err
	}

	dc.AddMach(mach)

	return mach, nil
}

func (dc *cluster) vmname() string {
	b := make([]byte, 5)
	rand.Read(b)
	return fmt.Sprintf("%s-%x", dc.Name()[0:13], b)
}

func (dc *cluster) Destroy() {
	if err := dc.api.DeleteKey(context.TODO(), dc.sshKeyID); err != nil {
		plog.Errorf("Error deleting key %v: %v", dc.sshKeyID, err)
	}

	dc.BaseCluster.Destroy()
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
