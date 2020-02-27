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
	"strings"
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
	"github.com/coreos/mantle/util"
)

const (
	// Provisioning a VM is supposed to take < 8 minutes, but in practice can take longer.
	launchTimeout       = 10 * time.Minute
	launchPollInterval  = 30 * time.Second
	installTimeout      = 15 * time.Minute
	installPollInterval = 5 * time.Second
	apiRetries          = 3
	apiRetryInterval    = 5 * time.Second
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "platform/api/packet")

	defaultInstallerImageBaseURL = map[string]string{
		// HTTPS causes iPXE to fail on a "permission denied" error
		"amd64-usr": "http://stable.release.core-os.net/amd64-usr/current",
		"arm64-usr": "http://beta.release.core-os.net/arm64-usr/current",
	}
	defaultImageURL = map[string]string{
		"amd64-usr": "https://alpha.release.core-os.net/amd64-usr/current/coreos_production_packet_image.bin.bz2",
		"arm64-usr": "https://alpha.release.core-os.net/arm64-usr/current/coreos_production_packet_image.bin.bz2",
	}
	defaultPlan = map[string]string{
		"amd64-usr": "t1.small.x86",
		"arm64-usr": "c1.large.arm",
	}
	linuxConsole = map[string]string{
		"amd64-usr":   "ttyS1,115200",
		"arm64-usr":   "ttyAMA0,115200",
		"s390x-usr":   "ttysclp0,115200",
		"ppc64le-usr": "hvc0,115200",
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
	// Slug of the device type (e.g. "t1.small.x86")
	Plan string
	// The Container Linux board name
	Board string
	// e.g. http://alpha.release.core-os.net/amd64-usr/current
	InstallerImageBaseURL string
	// e.g. https://alpha.release.core-os.net/amd64-usr/current/coreos_production_packet_image.bin.bz2
	ImageURL string

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
	if opts.InstallerImageBaseURL == "" {
		opts.InstallerImageBaseURL = defaultInstallerImageBaseURL[opts.Board]
	}
	if opts.ImageURL == "" {
		opts.ImageURL = defaultImageURL[opts.Board]
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

func (a *API) ListKeys() ([]packngo.SSHKey, error) {
	keys, _, err := a.c.SSHKeys.List()
	if err != nil {
		return nil, fmt.Errorf("couldn't list SSH keys: %v", err)
	}
	return keys, nil
}

func (a *API) wrapUserData(conf *conf.Conf) (string, error) {
	userDataOption := "-i"
	if !conf.IsIgnition() && conf.String() != "" {
		// By providing a no-op Ignition config, we prevent Ignition
		// from enabling oem-cloudinit.service, which is unordered
		// with respect to the cloud-config installed by the -c
		// option. Otherwise it might override settings in the
		// cloud-config with defaults obtained from the Packet
		// metadata endpoint.
		userDataOption = "-i /noop.ign -c"
	}
	escapedImageURL := strings.Replace(a.opts.ImageURL, "%", "%%", -1)

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

ExecStart=/usr/bin/curl -fo image.bin.bz2 "%v"
# We don't verify signatures because the iPXE script isn't verified either
# (and, in fact, is transferred over HTTP)

ExecStart=/usr/bin/coreos-install -d /dev/sda -f image.bin.bz2 %v /userdata

ExecStart=/usr/bin/systemctl --no-block isolate reboot.target

StandardOutput=journal+console
StandardError=journal+console

[Install]
RequiredBy=multi-user.target
`, escapedImageURL, userDataOption)

	// make workarounds
	noopIgnitionConfig := base64.StdEncoding.EncodeToString([]byte(`{"ignition": {"version": "2.1.0"}}`))
	coreosCloudInit := base64.StdEncoding.EncodeToString([]byte("#!/bin/sh\nexit 0"))

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
					Mode: 0644,
				},
				ignition.File{
					Filesystem: "root",
					Path:       "/noop.ign",
					Contents: ignition.FileContents{
						Source: ignition.Url{
							Scheme: "data",
							Opaque: ";base64," + noopIgnitionConfig,
						},
					},
					Mode: 0644,
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
boot`, strings.TrimRight(a.opts.InstallerImageBaseURL, "/"), userdataURL, linuxConsole[a.opts.Board])
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
			Hostname:      hostname,
			OS:            "custom_ipxe",
			IPXEScriptURL: ipxeScriptURL,
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
	var device *packngo.Device
	err := util.WaitUntilReady(launchTimeout, launchPollInterval, func() (bool, error) {
		var err error
		device, _, err = a.c.Devices.Get(deviceID)
		if err != nil {
			return false, fmt.Errorf("querying device: %v", err)
		}
		return device.State == "active", nil
	})
	if err != nil {
		return nil, err
	}
	return device, nil
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

func (a *API) GC(gracePeriod time.Duration) error {
	threshold := time.Now().Add(-gracePeriod)

	page := packngo.ListOptions{
		Page:    1,
		PerPage: 1000,
	}

	for {
		devices, _, err := a.c.Devices.List(a.opts.Project, &page)
		if err != nil {
			return fmt.Errorf("listing devices: %v", err)
		}
		for _, device := range devices {
			tagged := false
			for _, tag := range device.Tags {
				if tag == "mantle" {
					tagged = true
					break
				}
			}
			if !tagged {
				continue
			}

			switch device.State {
			case "queued", "provisioning":
				continue
			}

			if device.Locked {
				continue
			}

			created, err := time.Parse(time.RFC3339, device.Created)
			if err != nil {
				return fmt.Errorf("couldn't parse %q: %v", device.Created, err)
			}
			if created.After(threshold) {
				continue
			}

			if err := a.DeleteDevice(device.ID); err != nil {
				return fmt.Errorf("couldn't delete device %v: %v", device.ID, err)
			}
		}
		if len(devices) < page.PerPage {
			return nil
		}
		page.Page += 1
	}
}
