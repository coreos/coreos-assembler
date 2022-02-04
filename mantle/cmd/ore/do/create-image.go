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
	"os"
	"time"

	"github.com/pborman/uuid"
	"github.com/spf13/cobra"

	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/conf"
	"github.com/coreos/mantle/util"
)

var (
	cmdCreateImage = &cobra.Command{
		Use:   "create-image [options]",
		Short: "Create image",
		Long:  `Create an image.`,
		RunE:  runCreateImage,

		SilenceUsage: true,
	}

	customImage bool
)

func init() {
	DO.AddCommand(cmdCreateImage)
	cmdCreateImage.Flags().StringVar(&options.Region, "region", "sfo2", "region slug")
	cmdCreateImage.Flags().StringVarP(&imageName, "name", "n", "", "image name")
	cmdCreateImage.Flags().StringVarP(&imageURL, "url", "u", "", "image source URL (e.g. \"https://stable.release.core-os.net/amd64-usr/current/coreos_production_digitalocean_image.bin.bz2\"")
	cmdCreateImage.Flags().BoolVarP(&customImage, "custom", "", false, "create a \"custom image\" (which supports DHCP) rather than a snapshot of a distribution image (which doesn't)")
}

func runCreateImage(cmd *cobra.Command, args []string) error {
	if len(args) != 0 {
		fmt.Fprintf(os.Stderr, "Unrecognized args in do create-image cmd: %v\n", args)
		os.Exit(2)
	}

	if err := createImage(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	return nil
}

func createImage() error {
	if imageName == "" {
		return fmt.Errorf("Image name must be specified")
	}
	if imageURL == "" {
		return fmt.Errorf("Image URL must be specified")
	}

	if customImage {
		return createCustomImage()
	} else {
		return createSnapshot()
	}
}

func createCustomImage() error {
	ctx := context.Background()

	if _, err := API.CreateCustomImage(ctx, imageName, imageURL); err != nil {
		return fmt.Errorf("couldn't create image: %v", err)
	}

	return nil
}

func createSnapshot() error {
	// set smallest available size, so the image will run on any size droplet
	options.Size = "512mb"

	userdata, err := makeUserData()
	if err != nil {
		return err
	}

	ctx := context.Background()

	key, err := platform.GenerateFakeKey()
	if err != nil {
		return err
	}
	keyID, err := API.AddKey(ctx, "ore-"+uuid.New(), key)
	if err != nil {
		return err
	}
	defer func() {
		err := API.DeleteKey(ctx, keyID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
		}
	}()

	droplet, err := API.CreateDroplet(ctx, imageName+"-install", keyID, userdata)
	if err != nil {
		return fmt.Errorf("couldn't create droplet: %v", err)
	}
	dropletID := droplet.ID
	defer func() {
		err := API.DeleteDroplet(ctx, dropletID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
		}
	}()

	// the droplet will power itself off when install completes
	err = util.WaitUntilReady(10*time.Minute, 15*time.Second, func() (bool, error) {
		droplet, err := API.GetDroplet(ctx, dropletID)
		if err != nil {
			return false, err
		}
		return droplet.Status == "off", nil
	})
	if err != nil {
		return fmt.Errorf("Failed waiting for droplet to power off (%v). Did install fail?", err)
	}

	if err := API.SnapshotDroplet(ctx, dropletID, imageName); err != nil {
		return fmt.Errorf("couldn't snapshot droplet: %v", err)
	}

	return nil
}

func makeUserData() (string, error) {
	// TODO: This needs to be updated for Fedora CoreOS
	config := fmt.Sprintf(`storage:
  files:
    - filesystem: root
      path: /root/initramfs/etc/resolv.conf
      mode: 0644
      contents:
        inline: nameserver 8.8.8.8
    - filesystem: root
      path: /root/initramfs/shutdown
      mode: 0755
      contents:
        inline: |
          #!/busybox sh

          set -e -o pipefail

          echo "Starting install..."
          disk=$(/busybox mountpoint -n /oldroot | /busybox sed -e 's/p*[0-9]* .*//')

          echo "Unmounting filesystems..."
          /busybox find /oldroot -depth -type d -exec /busybox mountpoint -q {} \; -exec /busybox umount {} \;
          # Verify success
          /busybox mountpoint -q /oldroot && /busybox false

          echo "Zeroing ${disk}..."
          /busybox dd if=/dev/zero of="${disk}" bs=1M ||:

          echo "Installing to ${disk}..."
          /busybox wget -O - "%s" | \
              /busybox bunzip2 -c | \
              /busybox dd of="${disk}" bs=1M

          echo "Shutting down..."
          /busybox poweroff -f
systemd:
  units:
    - name: install-prep.service
      enabled: true
      contents: |
        [Unit]
        Description=Launch Install
        After=multi-user.target

        [Service]
        Type=oneshot

        # https://github.com/coreos/bugs/issues/2205
        ExecStart=/usr/bin/wget -O /root/initramfs/busybox https://busybox.net/downloads/binaries/1.27.1-i686/busybox
        ExecStart=/bin/sh -c 'echo "b51b9328eb4e60748912e1c1867954a5cf7e9d5294781cae59ce225ed110523c /root/initramfs/busybox" | sha256sum -c -'
        ExecStart=/usr/bin/chmod +x /root/initramfs/busybox

        ExecStart=/usr/bin/rsync -a /root/initramfs/ /run/initramfs
        ExecStart=/usr/bin/systemctl --no-block poweroff

        [Install]
        WantedBy=multi-user.target
`, imageURL)

	conf, err := conf.Ignition(config).Render(conf.ReportWarnings)
	if err != nil {
		return "", fmt.Errorf("Couldn't render userdata: %v", err)
	}
	return conf.String(), nil
}
