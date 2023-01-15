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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	ignition "github.com/coreos/ignition/v2/config/v3_0/types"
	"github.com/coreos/pkg/capnslog"
	"github.com/packethost/packngo"
	"golang.org/x/crypto/ssh"

	"github.com/coreos/coreos-assembler/mantle/auth"
	"github.com/coreos/coreos-assembler/mantle/fcos"
	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
	"github.com/coreos/coreos-assembler/mantle/util"
)

const (
	// Provisioning a VM is supposed to take < 8 minutes, but in practice can take longer.
	launchTimeout       = 10 * time.Minute
	launchPollInterval  = 30 * time.Second
	installTimeout      = 15 * time.Minute
	installPollInterval = 5 * time.Second
	apiRetries          = 3
	apiRetryInterval    = 5 * time.Second

	imageDefaultStream = "testing"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/coreos-assembler/mantle", "platform/api/packet")

	defaultPlan = map[string]string{
		"arm64":  "c1.large.arm",
		"x86_64": "t1.small.x86",
	}
	defaultIPXEURL = map[string]string{
		"x86_64": "https://raw.githubusercontent.com/coreos/coreos-assembler/main/mantle/platform/api/packet/fcos-x86_64.ipxe",
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
	// CPU architecture
	Architecture string
	// e.g. https://raw.githubusercontent.com/coreos/coreos-assembler/main/mantle/platform/api/packet/fcos-x86_64.ipxe
	IPXEURL string
	// e.g. https://builds.coreos.fedoraproject.org/prod/streams/stable/builds/31.20200223.3.0/x86_64/fedora-coreos-31.20200223.3.0-metal.x86_64.raw.xz
	ImageURL string
}

type API struct {
	c    *packngo.Client
	opts *Options
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

	_, ok := defaultPlan[opts.Architecture]
	if !ok {
		return nil, fmt.Errorf("unknown architecture %q", opts.Architecture)
	}
	if opts.Plan == "" {
		opts.Plan = defaultPlan[opts.Architecture]
	}
	if opts.IPXEURL == "" {
		opts.IPXEURL = defaultIPXEURL[opts.Architecture]
	}
	if opts.ImageURL == "" {
		artifacts, err := fcos.FetchCanonicalStreamArtifacts(imageDefaultStream, opts.Architecture)
		if err != nil {
			return nil, err
		}

		metal, ok := artifacts.Artifacts["metal"]
		if !ok {
			return nil, fmt.Errorf("stream metadata missing metal image")
		}
		f, ok := metal.Formats["raw.xz"]
		if !ok {
			return nil, fmt.Errorf("stream metadata missing raw.xz format")
		}
		d := f.Disk
		if d == nil {
			return nil, fmt.Errorf("stream metadata missing raw.xz format disk")
		}
		opts.ImageURL = d.Location
	}

	client := packngo.NewClient("github.com/coreos/coreos-assembler", opts.ApiKey, nil)

	return &API{
		c:    client,
		opts: opts,
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

	device, err := a.createDevice(hostname, userdata)
	if err != nil {
		return nil, fmt.Errorf("couldn't create device: %v", err)
	}
	deviceID := device.ID

	if console != nil {
		err := a.startConsole(deviceID, console)
		consoleStarted = true
		if err != nil {
			if errDelete := a.DeleteDevice(deviceID); errDelete != nil {
				return nil, fmt.Errorf("deleting device failed: %v after starting console failed: %v", errDelete, err)
			}
			return nil, err
		}
	}

	device, err = a.waitForActive(deviceID)
	if err != nil {
		if errDelete := a.DeleteDevice(deviceID); errDelete != nil {
			return nil, fmt.Errorf("deleting device failed: %v after waiting for device to be active failed: %v", errDelete, err)
		}
		return nil, err
	}

	ipAddress := a.GetDeviceAddress(device, 4, true)
	if ipAddress == "" {
		if errDelete := a.DeleteDevice(deviceID); errDelete != nil {
			return nil, fmt.Errorf("deleting device failed: %v after no public IP address found for %v", errDelete, err)
		}
		return nil, fmt.Errorf("no public IP address found for %v", deviceID)
	}

	err = waitForInstall(ipAddress)
	if err != nil {
		if errDelete := a.DeleteDevice(deviceID); errDelete != nil {
			return nil, fmt.Errorf("deleting device failed: %v after timed out waiting for coreos-installer: %v", errDelete, err)
		}
		return nil, fmt.Errorf("timed out waiting for coreos-installer: %v", err)
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
Description=Run coreos-installer

Requires=network-online.target
After=network-online.target

Requires=dev-sda.device
After=dev-sda.device

[Service]
Type=oneshot

# We don't verify signatures because this might be a random dev image.
# Even if a signature exists, there's no way to verify signatures on the
# iPXE script or PXE image, so we have to trust the web server.
ExecStart=/usr/bin/coreos-installer install -u %q -i /var/userdata -p packet --insecure /dev/sda

ExecStart=/usr/bin/systemctl reboot

StandardOutput=journal+console
StandardError=journal+console

[Install]
RequiredBy=multi-user.target
`, escapedImageURL)

	// make Ignition config
	b64UserData := base64.StdEncoding.EncodeToString(conf.Bytes())
	var buf bytes.Buffer
	err := json.NewEncoder(&buf).Encode(ignition.Config{
		Ignition: ignition.Ignition{
			Version: "3.0.0",
		},
		Storage: ignition.Storage{
			Files: []ignition.File{
				{
					Node: ignition.Node{
						Path: "/var/userdata",
					},
					FileEmbedded1: ignition.FileEmbedded1{
						Contents: ignition.FileContents{
							Source: util.StrToPtr("data:;base64," + b64UserData),
						},
						Mode: util.IntToPtr(0644),
					},
				},
			},
		},
		Systemd: ignition.Systemd{
			Units: []ignition.Unit{
				{
					// don't appear to be running while install is in progress
					Name: "sshd.service",
					Mask: util.BoolToPtr(true),
				},
				{
					// allow remote detection of install in progress
					Name:     "discard.socket",
					Enabled:  util.BoolToPtr(true),
					Contents: util.StrToPtr(discardSocketUnit),
				},
				{
					Name:     "discard@.service",
					Contents: util.StrToPtr(discardServiceUnit),
				},
				{
					Name:     "coreos-installer.service",
					Enabled:  util.BoolToPtr(true),
					Contents: util.StrToPtr(installUnit),
				},
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("encoding Ignition config: %v", err)
	}

	return buf.String(), nil
}

// device creation seems a bit flaky, so try a few times
func (a *API) createDevice(hostname, userdata string) (device *packngo.Device, err error) {
	for tries := apiRetries; tries >= 0; tries-- {
		var response *packngo.Response
		device, response, err = a.c.Devices.Create(&packngo.DeviceCreateRequest{
			ProjectID:     a.opts.Project,
			Facility:      a.opts.Facility,
			Plan:          a.opts.Plan,
			BillingCycle:  "hourly",
			UserData:      userdata,
			Hostname:      hostname,
			OS:            "custom_ipxe",
			IPXEScriptURL: a.opts.IPXEURL,
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
			conn.SetDeadline(deadline) //nolint
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
			time.Sleep(installPollInterval - time.Since(start))
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
