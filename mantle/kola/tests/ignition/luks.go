package ignition

import (
	"fmt"
	"time"

	"github.com/coreos/mantle/kola"
	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	ut "github.com/coreos/mantle/kola/tests/util"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/conf"
	"github.com/coreos/mantle/platform/machine/unprivqemu"
	"github.com/coreos/mantle/system"
	"github.com/coreos/mantle/util"
)

func init() {
	// Create 0 cluster size to allow starting and setup of Tang as needed per test
	// See: https://github.com/coreos/coreos-assembler/pull/1310#discussion_r401908836
	register.RegisterTest(&register.Test{
		Run:         luksTangTest,
		ClusterSize: 0,
		Name:        `luks.tang`,
		Flags:       []register.Flag{},
		Distros:     []string{"rhcos"},
		Tags:        []string{"luks", "tang", kola.NeedsInternetTag},
	})
	register.RegisterTest(&register.Test{
		Run:                  luksSSST1Test,
		ClusterSize:          0,
		Name:                 `luks.sss.t1`,
		Flags:                []register.Flag{},
		Distros:              []string{"rhcos"},
		Platforms:            []string{"qemu-unpriv"},
		ExcludeArchitectures: []string{"s390x"}, // no TPM backend support for s390x
		Tags:                 []string{"luks", "tpm", "tang", "sss", kola.NeedsInternetTag},
	})
	register.RegisterTest(&register.Test{
		Run:                  luksSSST2Test,
		ClusterSize:          0,
		Name:                 `luks.sss.t2`,
		Flags:                []register.Flag{},
		Distros:              []string{"rhcos"},
		Platforms:            []string{"qemu-unpriv"},
		ExcludeArchitectures: []string{"s390x"}, // no TPM backend support for s390x
		Tags:                 []string{"luks", "tpm", "tang", "sss", kola.NeedsInternetTag},
	})
}

func setupTangMachine(c cluster.TestCluster) ut.TangServer {
	var m platform.Machine
	var err error
	var thumbprint []byte
	var tangAddress string

	options := platform.QemuMachineOptions{
		HostForwardPorts: []platform.HostForwardPort{
			{Service: "ssh", HostPort: 0, GuestPort: 22},
			{Service: "tang", HostPort: 0, GuestPort: 80},
		},
	}

	ignition := conf.Ignition(`{
		"ignition": {
			"version": "3.0.0"
		}
	}`)

	switch pc := c.Cluster.(type) {
	// These cases have to be separated because when put together to the same case statement
	// the golang compiler no longer checks that the individual types in the case have the
	// NewMachineWithQemuOptions function, but rather whether platform.Cluster
	// does which fails
	case *unprivqemu.Cluster:
		m, err = pc.NewMachineWithQemuOptions(ignition, options)
		for _, hfp := range options.HostForwardPorts {
			if hfp.Service == "tang" {
				tangAddress = fmt.Sprintf("10.0.2.2:%d", hfp.HostPort)
			}
		}
	default:
		m, err = pc.NewMachine(ignition)
		tangAddress = fmt.Sprintf("%s:80", m.PrivateIP())
	}
	if err != nil {
		c.Fatal(err)
	}

	// TODO: move container image to centralized namespace
	// container source: https://github.com/mike-nguyen/tang-docker-container/
	containerImage := "quay.io/mike_nguyen/tang"
	if system.RpmArch() != "x86_64" {
		containerImage = "quay.io/multi-arch/tang:" + system.RpmArch()
	}

	containerID, errMsg, err := m.SSH("sudo podman run -d -p 80:80 " + containerImage)
	if err != nil {
		c.Fatalf("Unable to start Tang container: %v\n%s", err, string(errMsg))
	}

	// Wait a little bit for the container to start
	if err := util.Retry(10, time.Second, func() error {
		cmd := fmt.Sprintf("sudo podman exec %s /usr/bin/tang-show-keys", string(containerID))
		thumbprint, _, err = m.SSH(cmd)
		if err != nil {
			return err
		}
		if string(thumbprint) == "" {
			return fmt.Errorf("tang-show-keys returns nothing")
		}
		return nil
	}); err != nil {
		c.Fatalf("Unable to retrieve Tang keys: %v", err)
	}

	return ut.TangServer{
		Machine:    m,
		Address:    tangAddress,
		Thumbprint: string(thumbprint),
	}
}

func runTest(c cluster.TestCluster, tpm2 bool, threshold int, killTangAfterFirstBoot bool) {
	tangd := setupTangMachine(c)
	ignition := conf.Ignition(fmt.Sprintf(`{
		"ignition": {
			"version": "3.2.0"
		},
		"storage": {
			"luks": [
				{
					"name": "root",
					"device": "/dev/disk/by-label/root",
					"clevis": {
						"tpm2": %v,
						"tang": [
							{
								"url": "http://%s",
								"thumbprint": "%s"
							}
						],
						"threshold": %d
					},
					"label": "root",
					"wipeVolume": true
				}
			],
			"filesystems": [
				{
					"device": "/dev/mapper/root",
					"format": "xfs",
					"wipeFilesystem": true,
					"label": "root"
				}
			]
		}
	}`, tpm2, tangd.Address, tangd.Thumbprint, threshold))

	opts := platform.MachineOptions{
		MinMemory: 4096,
	}
	// ppc64le and aarch64 use 64K pages
	switch system.RpmArch() {
	case "ppc64le", "aarch64":
		opts.MinMemory = 8192
	}
	m, err := c.NewMachineWithOptions(ignition, opts)
	if err != nil {
		c.Fatalf("Unable to create test machine: %v", err)
	}
	rootPart := "/dev/disk/by-partlabel/root"
	// hacky,  but needed for s390x because of gpt issue with naming on big endian systems: https://bugzilla.redhat.com/show_bug.cgi?id=1899990
	if system.RpmArch() == "s390x" {
		rootPart = "/dev/disk/by-id/virtio-primary-disk-part4"
	}
	ut.LUKSSanityTest(c, tangd, m, tpm2, killTangAfterFirstBoot, rootPart)
}

// Verify that the rootfs is encrypted with Tang
func luksTangTest(c cluster.TestCluster) {
	runTest(c, false, 1, false)
}

// Verify that the rootfs is encrypted with SSS with t=1
func luksSSST1Test(c cluster.TestCluster) {
	runTest(c, true, 1, true)
}

// Verify that the rootfs is encrypted with SSS with t=2
func luksSSST2Test(c cluster.TestCluster) {
	runTest(c, true, 2, false)
}
