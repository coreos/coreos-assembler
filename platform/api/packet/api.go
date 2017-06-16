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

package packet

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"time"

	ignition "github.com/coreos/ignition/config/v2_0/types"
	"github.com/coreos/pkg/capnslog"
	"github.com/packethost/packngo"
	"golang.org/x/crypto/ssh"
	"golang.org/x/net/context"
	gs "google.golang.org/api/storage/v1"

	"github.com/coreos/mantle/auth"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/api/gcloud"
	"github.com/coreos/mantle/platform/conf"
	"github.com/coreos/mantle/storage"
)

const (
	// Provisioning a VM is supposed to take < 8 minutes.
	launchTimeout       = 10 * time.Minute
	launchPollInterval  = 30 * time.Second
	installTimeout      = 10 * time.Minute
	installPollInterval = 5 * time.Second
	apiRetries          = 3
	apiRetryInterval    = 5 * time.Second
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "platform/api/packet")

	defaultInstallerImageURL = map[string]string{
		// HTTPS causes iPXE to fail on a "permission denied" error
		"amd64-usr": "http://stable.release.core-os.net/amd64-usr/current",
		"arm64-usr": "http://beta.release.core-os.net/arm64-usr/current",
	}
	defaultImageBaseURL = map[string]string{
		"amd64-usr": "https://alpha.release.core-os.net/amd64-usr",
		"arm64-usr": "https://alpha.release.core-os.net/arm64-usr",
	}
	defaultPlan = map[string]string{
		"amd64-usr": "baremetal_0",
		"arm64-usr": "baremetal_2a",
	}
	linuxConsole = map[string]string{
		"amd64-usr": "ttyS1,115200",
		"arm64-usr": "ttyAMA0,115200",
	}
)

type Options struct {
	*platform.Options

	// Config file. Defaults to $HOME/.config/packet.json.
	ConfigPath string
	// Profile name
	Profile string
	// API key (overrides config profile)
	ApiKey string
	// Project UUID (overrides config profile)
	Project string

	// Packet location code
	Facility string
	// Slug of the device type (e.g. "baremetal_0")
	Plan string
	// The Container Linux board name
	Board string
	// e.g. http://alpha.release.core-os.net/amd64-usr/current
	InstallerImageURL string
	// e.g. https://alpha.release.core-os.net/amd64-usr
	ImageBaseURL string
	// Version number or "current"
	ImageVersion string

	// Options for Google Storage
	GSOptions *gcloud.Options
	// Google Storage base URL for temporary uploads
	// e.g. gs://users.developer.core-os.net/bovik/mantle
	StorageURL string
}

type API struct {
	c      *packngo.Client
	bucket *storage.Bucket
	opts   *Options
}

type Console interface {
	io.WriteCloser
	SSHClient(ip, user string) (*ssh.Client, error)
}

func New(opts *Options) (*API, error) {
	if opts.ApiKey == "" || opts.Project == "" {
		profiles, err := auth.ReadPacketConfig(opts.ConfigPath)
		if err != nil {
			return nil, fmt.Errorf("couldn't read Packet config: %v", err)
		}

		if opts.Profile == "" {
			opts.Profile = "default"
		}
		profile, ok := profiles[opts.Profile]
		if !ok {
			return nil, fmt.Errorf("no such profile %q", opts.Profile)
		}
		if opts.ApiKey == "" {
			opts.ApiKey = profile.ApiKey
		}
		if opts.Project == "" {
			opts.Project = profile.Project
		}
	}

	_, ok := linuxConsole[opts.Board]
	if !ok {
		return nil, fmt.Errorf("unknown board %q", opts.Board)
	}
	if opts.Plan == "" {
		opts.Plan = defaultPlan[opts.Board]
	}
	if opts.InstallerImageURL == "" {
		opts.InstallerImageURL = defaultInstallerImageURL[opts.Board]
	}
	if opts.ImageBaseURL == "" {
		opts.ImageBaseURL = defaultImageBaseURL[opts.Board]
	}

	gapi, err := gcloud.New(opts.GSOptions)
	if err != nil {
		return nil, fmt.Errorf("connecting to Google Storage: %v", err)
	}
	bucket, err := storage.NewBucket(gapi.Client(), opts.StorageURL)
	if err != nil {
		return nil, fmt.Errorf("connecting to Google Storage bucket: %v", err)
	}

	client := packngo.NewClient("github.com/coreos/mantle", opts.ApiKey, nil)

	return &API{
		c:      client,
		bucket: bucket,
		opts:   opts,
	}, nil
}

func (a *API) PreflightCheck() error {
	_, _, err := a.c.Projects.Get(a.opts.Project)
	if err != nil {
		return fmt.Errorf("querying project %v: %v", a.opts.Project, err)
	}
	return nil
}

// console is optional, and is closed on error or when the device is deleted.
func (a *API) CreateDevice(hostname string, conf *conf.Conf, console Console) (*packngo.Device, error) {
	consoleStarted := false
	defer func() {
		if console != nil && !consoleStarted {
			console.Close()
		}
	}()

	userdata, err := a.wrapUserData(conf)
	if err != nil {
		return nil, err
	}

	// The Ignition config can't go in userdata via coreos.config.url=https://metadata.packet.net/userdata because Ignition supplies an Accept header that metadata.packet.net finds 406 Not Acceptable.
	// It can't go in userdata via coreos.oem.id=packet because the Packet OEM expects unit files in /usr/share/oem which the PXE image doesn't have.
	// If metadata.packet.net is fixed and we move the Ignition config to userdata, "coreos-install -c /cloud-config" will also need "-i /empty" to prevent the installed Ignition from interpreting the userdata intended for the installer Ignition.
	userdataName, userdataURL, err := a.uploadObject(hostname, "application/vnd.coreos.ignition+json", []byte(userdata))
	if err != nil {
		return nil, err
	}
	defer a.bucket.Delete(context.TODO(), userdataName)

	// This can't go in userdata because the installed coreos-cloudinit will try to execute it.
	ipxeScriptName, ipxeScriptURL, err := a.uploadObject(hostname, "application/octet-stream", []byte(a.ipxeScript(userdataURL)))
	if err != nil {
		return nil, err
	}
	defer a.bucket.Delete(context.TODO(), ipxeScriptName)

	device, err := a.createDevice(hostname, ipxeScriptURL)
	if err != nil {
		return nil, fmt.Errorf("couldn't create device: %v", err)
	}
	deviceID := device.ID

	if console != nil {
		err := a.startConsole(deviceID, console)
		consoleStarted = true
		if err != nil {
			a.DeleteDevice(deviceID)
			return nil, err
		}
	}

	device, err = a.waitForActive(deviceID)
	if err != nil {
		a.DeleteDevice(deviceID)
		return nil, err
	}

	ipAddress := a.GetDeviceAddress(device, 4, true)
	if ipAddress == "" {
		a.DeleteDevice(deviceID)
		return nil, fmt.Errorf("no public IP address found for %v", deviceID)
	}

	err = waitForInstall(ipAddress)
	if err != nil {
		a.DeleteDevice(deviceID)
		return nil, fmt.Errorf("timed out waiting for coreos-install: %v", err)
	}

	return device, nil
}

func (a *API) DeleteDevice(deviceID string) error {
	_, err := a.c.Devices.Delete(deviceID)
	if err != nil {
		return fmt.Errorf("deleting device %q: %v", deviceID, err)
	}
	return nil
}

func (a *API) GetDeviceAddress(device *packngo.Device, family int, public bool) string {
	for _, address := range device.Network {
		if address.AddressFamily == family && address.Public == public {
			return address.Address
		}
	}
	return ""
}

func (a *API) AddKey(name, key string) (string, error) {
	sshKey, _, err := a.c.SSHKeys.Create(&packngo.SSHKeyCreateRequest{
		Label: name,
		Key:   key,
	})
	if err != nil {
		return "", fmt.Errorf("couldn't create SSH key: %v", err)
	}
	return sshKey.ID, nil
}

func (a *API) DeleteKey(keyID string) error {
	_, err := a.c.SSHKeys.Delete(keyID)
	if err != nil {
		return fmt.Errorf("couldn't delete SSH key: %v", err)
	}
	return nil
}

func (a *API) wrapUserData(conf *conf.Conf) (string, error) {
	userDataOption := "-i"
	if !conf.IsIgnition() && conf.String() != "" {
		userDataOption = "-c"
	}

	// make systemd units
	discardSocketUnit := `
[Unit]
Description=Discard Socket

[Socket]
ListenStream=0.0.0.0:9
Accept=true

[Install]
WantedBy=multi-user.target
`
	discardServiceUnit := `
[Unit]
Description=Discard Service
Requires=discard.socket

[Service]
ExecStart=/usr/bin/cat
StandardInput=socket
StandardOutput=null
`
	installUnit := fmt.Sprintf(`
[Unit]
Description=Install Container Linux

Requires=network-online.target
After=network-online.target

Requires=dev-sda.device
After=dev-sda.device

[Service]
Type=oneshot
# Prevent coreos-install from validating cloud-config
Environment=PATH=/root/bin:/usr/sbin:/usr/bin

ExecStart=/root/bin/network-setup
ExecStart=/usr/bin/coreos-install -b "%v" -V "%v" -d /dev/sda -o packet -n %v /userdata

ExecStart=/usr/bin/mount /dev/sda6 /mnt
ExecStart=/bin/bash -c 'echo "set linux_console=\\\"console=%v\\\"" >> /mnt/grub.cfg'
# Work around coreos-install bug in Container Linux < 1381.0.0
ExecStart=/bin/bash -c 'echo "set linux_append=\\\"\$linux_append coreos.oem.id=packet\\\"" >> /mnt/grub.cfg'
ExecStart=/usr/bin/umount /mnt

ExecStart=/usr/bin/systemctl --no-block isolate reboot.target

StandardOutput=journal+console
StandardError=journal+console

[Install]
RequiredBy=multi-user.target
`, a.opts.ImageBaseURL, a.opts.ImageVersion, userDataOption, linuxConsole[a.opts.Board])

	// make workarounds
	coreosCloudInit := base64.StdEncoding.EncodeToString([]byte("#!/bin/sh\nexit 0"))
	// If we want a private IPv4 address, we need to set it up ourselves
	networkSetup := base64.StdEncoding.EncodeToString([]byte(`#!/bin/bash

set -e

metadata=$(curl --silent https://metadata.packet.net/metadata)
q() {
	jq -r "$1" <<<$metadata
}

cat > /run/systemd/network/00-mantle.network <<EOF
[Match]
MACAddress=$(q '.network.interfaces[0].mac')

[Network]
Address=$(q '[.network.addresses[] | select(.address_family == 4)][0].address')/$(q '[.network.addresses[] | select(.address_family == 4)][0].cidr')
Address=$(q '[.network.addresses[] | select(.address_family == 4)][1].address')/$(q '[.network.addresses[] | select(.address_family == 4)][1].cidr')
DNS=8.8.8.8
DNS=8.8.4.4

[Route]
Destination=0.0.0.0/0
Gateway=$(q '.network.addresses[] | select(.public == true and .address_family == 4).gateway')

[Route]
Destination=10.0.0.0/8
Gateway=$(q '.network.addresses[] | select(.public == false and .address_family == 4).gateway')
EOF
`))

	// make Ignition config
	b64UserData := base64.StdEncoding.EncodeToString(conf.Bytes())
	var buf bytes.Buffer
	err := json.NewEncoder(&buf).Encode(ignition.Config{
		Ignition: ignition.Ignition{
			Version: ignition.IgnitionVersion{Major: 2},
		},
		Storage: ignition.Storage{
			Files: []ignition.File{
				ignition.File{
					Filesystem: "root",
					Path:       "/userdata",
					Contents: ignition.FileContents{
						Source: ignition.Url{
							Scheme: "data",
							Opaque: ";base64," + b64UserData,
						},
					},
				},
				ignition.File{
					Filesystem: "root",
					Path:       "/root/bin/coreos-cloudinit",
					Contents: ignition.FileContents{
						Source: ignition.Url{
							Scheme: "data",
							Opaque: ";base64," + coreosCloudInit,
						},
					},
					Mode: 0755,
				},
				ignition.File{
					Filesystem: "root",
					Path:       "/root/bin/network-setup",
					Contents: ignition.FileContents{
						Source: ignition.Url{
							Scheme: "data",
							Opaque: ";base64," + networkSetup,
						},
					},
					Mode: 0755,
				},
			},
		},
		Systemd: ignition.Systemd{
			Units: []ignition.SystemdUnit{
				ignition.SystemdUnit{
					// don't appear to be running while install is in progress
					Name: "sshd.socket",
					Mask: true,
				},
				ignition.SystemdUnit{
					// future-proofing
					Name: "sshd.service",
					Mask: true,
				},
				ignition.SystemdUnit{
					// allow remote detection of install in progress
					Name:     "discard.socket",
					Enable:   true,
					Contents: discardSocketUnit,
				},
				ignition.SystemdUnit{
					Name:     "discard@.service",
					Contents: discardServiceUnit,
				},
				ignition.SystemdUnit{
					Name:     "coreos-install.service",
					Enable:   true,
					Contents: installUnit,
				},
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("encoding Ignition config: %v", err)
	}

	return buf.String(), nil
}

func (a *API) uploadObject(hostname, contentType string, data []byte) (string, string, error) {
	if hostname == "" {
		hostname = "mantle"
	}
	b := make([]byte, 5)
	rand.Read(b)
	name := fmt.Sprintf("%s-%x", hostname, b)

	obj := gs.Object{
		Name:        a.bucket.Prefix() + name,
		ContentType: contentType,
	}
	err := a.bucket.Upload(context.TODO(), &obj, bytes.NewReader(data))
	if err != nil {
		return "", "", fmt.Errorf("uploading object: %v", err)
	}

	// HTTPS causes iPXE to fail on a "permission denied" error
	url := fmt.Sprintf("http://storage-download.googleapis.com/%v/%v", a.bucket.Name(), obj.Name)
	return obj.Name, url, nil
}

func (a *API) ipxeScript(userdataURL string) string {
	return fmt.Sprintf(`#!ipxe
set base-url %s
kernel ${base-url}/coreos_production_pxe.vmlinuz initrd=coreos_production_pxe_image.cpio.gz coreos.first_boot=1 coreos.config.url=%s console=%s
initrd ${base-url}/coreos_production_pxe_image.cpio.gz
boot`, a.opts.InstallerImageURL, userdataURL, linuxConsole[a.opts.Board])
}

// device creation seems a bit flaky, so try a few times
func (a *API) createDevice(hostname, ipxeScriptURL string) (device *packngo.Device, err error) {
	for tries := apiRetries; tries >= 0; tries-- {
		var response *packngo.Response
		device, response, err = a.c.Devices.Create(&packngo.DeviceCreateRequest{
			ProjectID:     a.opts.Project,
			Facility:      a.opts.Facility,
			Plan:          a.opts.Plan,
			BillingCycle:  "hourly",
			HostName:      hostname,
			OS:            "custom_ipxe",
			IPXEScriptUrl: ipxeScriptURL,
			Tags:          []string{"mantle"},
		})
		if err == nil || response.StatusCode != 500 {
			return
		}
		if tries > 0 {
			time.Sleep(apiRetryInterval)
		}
	}
	return
}

func (a *API) startConsole(deviceID string, console Console) error {
	ready := make(chan error)

	runner := func() error {
		defer console.Close()

		client, err := console.SSHClient("sos."+a.opts.Facility+".packet.net", deviceID)
		if err != nil {
			return fmt.Errorf("couldn't create SSH client for %s console: %v", deviceID, err)
		}
		defer client.Close()

		session, err := client.NewSession()
		if err != nil {
			return fmt.Errorf("couldn't create SSH session for %s console: %v", deviceID, err)
		}
		defer session.Close()

		reader, writer := io.Pipe()
		defer writer.Close()

		session.Stdin = reader
		session.Stdout = console
		if err := session.Shell(); err != nil {
			return fmt.Errorf("couldn't start shell for %s console: %v", deviceID, err)
		}

		// cause startConsole to return
		ready <- nil

		err = session.Wait()
		_, ok := err.(*ssh.ExitMissingError)
		if err != nil && !ok {
			plog.Errorf("%s console session failed: %v", deviceID, err)
		}
		return nil
	}
	go func() {
		err := runner()
		if err != nil {
			ready <- err
		}
	}()

	return <-ready
}

func (a *API) waitForActive(deviceID string) (*packngo.Device, error) {
	for tries := launchTimeout / launchPollInterval; tries >= 0; tries-- {
		device, _, err := a.c.Devices.Get(deviceID)
		if err != nil {
			return nil, fmt.Errorf("querying device: %v", err)
		}
		if device.State == "active" {
			return device, nil
		}
		if tries > 0 {
			time.Sleep(launchPollInterval)
		}
	}
	return nil, fmt.Errorf("timed out waiting for device")
}

// Connect to the discard port and wait for the connection to close,
// indicating that install is complete.
func waitForInstall(address string) (err error) {
	deadline := time.Now().Add(installTimeout)
	dialer := net.Dialer{
		Timeout: installPollInterval,
	}
	for tries := installTimeout / installPollInterval; tries >= 0; tries-- {
		var conn net.Conn
		start := time.Now()
		conn, err = dialer.Dial("tcp", address+":9")
		if err == nil {
			defer conn.Close()
			conn.SetDeadline(deadline)
			_, err = conn.Read([]byte{0})
			if err == io.EOF {
				err = nil
			}
			return
		}
		if tries > 0 {
			// If Dial returned an error before the timeout,
			// e.g. because the device returned ECONNREFUSED,
			// wait out the rest of the interval.
			time.Sleep(installPollInterval - time.Now().Sub(start))
		}
	}
	return
}
