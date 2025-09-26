package ignition

import (
	"fmt"
	"os"
	"time"

	coreosarch "github.com/coreos/stream-metadata-go/arch"

	"github.com/coreos/coreos-assembler/mantle/kola"
	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/kola/register"
	"github.com/coreos/coreos-assembler/mantle/kola/tests/coretest"
	ut "github.com/coreos/coreos-assembler/mantle/kola/tests/util"
	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
	"github.com/coreos/coreos-assembler/mantle/platform/machine/qemu"
	"github.com/coreos/coreos-assembler/mantle/util"
)

func init() {
	// Create 0 cluster size to allow starting and setup of Tang as needed per test
	// See: https://github.com/coreos/coreos-assembler/pull/1310#discussion_r401908836
	register.RegisterTest(&register.Test{
		Run:         luksTangTest,
		ClusterSize: 0,
		Name:        `luks.tang`,
		Description: "Verify that the rootfs is encrypted with Tang.",
		Flags:       []register.Flag{},
		Distros:     []string{"rhcos", "scos"},
		Tags:        []string{"luks", "tang", kola.NeedsInternetTag, "reprovision"},
	})
	register.RegisterTest(&register.Test{
		Run:                  luksSSST1Test,
		ClusterSize:          0,
		Name:                 `luks.sss.t1`,
		Description:          "Verify that the rootfs is encrypted with SSS with t=1.",
		Flags:                []register.Flag{},
		Distros:              []string{"rhcos", "scos"},
		Platforms:            []string{"qemu"},
		ExcludeArchitectures: []string{"s390x"}, // no TPM backend support for s390x
		Tags:                 []string{"luks", "tpm", "tang", "sss", kola.NeedsInternetTag, "reprovision"},
	})
	register.RegisterTest(&register.Test{
		Run:                  luksSSST2Test,
		ClusterSize:          0,
		Name:                 `luks.sss.t2`,
		Description:          "Verify that the rootfs is encrypted with SSS with t=2.",
		Flags:                []register.Flag{},
		Distros:              []string{"rhcos", "scos"},
		Platforms:            []string{"qemu"},
		ExcludeArchitectures: []string{"s390x"}, // no TPM backend support for s390x
		Tags:                 []string{"luks", "tpm", "tang", "sss", kola.NeedsInternetTag, "reprovision"},
	})
	register.RegisterTest(&register.Test{
		Run:           runCexTest,
		ClusterSize:   0,
		Name:          `luks.cex`,
		Description:   "Verify that CEX-based rootfs encryption works.",
		Flags:         []register.Flag{},
		Distros:       []string{"rhcos"},
		Platforms:     []string{"qemu"},
		Architectures: []string{"s390x"},
		Tags:          []string{"luks", "cex", "reprovision"},
		NativeFuncs: map[string]register.NativeFuncWrap{
			"RHCOSGrowpart": register.CreateNativeFuncWrap(coretest.TestRHCOSGrowfs, []string{"fcos"}...),
			"FCOSGrowpart":  register.CreateNativeFuncWrap(coretest.TestFCOSGrowfs, []string{"rhcos"}...),
		},
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
	case *qemu.Cluster:
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

	containerImage := "quay.io/coreos-assembler/tang:latest"

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
	// ppc64le uses 64K pages; see similar logic in harness.go and boot-mirror.go
	switch coreosarch.CurrentRpmArch() {
	case "ppc64le":
		opts.MinMemory = 8192
	}
	m, err := c.NewMachineWithOptions(ignition, opts)
	if err != nil {
		c.Fatalf("Unable to create test machine: %v", err)
	}
	rootPart := "/dev/disk/by-partlabel/root"
	// hacky,  but needed for s390x because of gpt issue with naming on big endian systems: https://bugzilla.redhat.com/show_bug.cgi?id=1899990
	if coreosarch.CurrentRpmArch() == "s390x" {
		rootPart = "/dev/disk/by-id/virtio-primary-disk-part4"
	}
	ut.LUKSSanityTest(c, tangd, m, tpm2, killTangAfterFirstBoot, rootPart)
}

func runCexTest(c cluster.TestCluster) {
	var err error
	var m platform.Machine

	// To prevent the test to fail the whole run on s390x machine that does not have Cex Device
	cex_uuid := os.Getenv("KOLA_CEX_UUID")
	if cex_uuid == "" {
		c.Skip("No CEX device found in KOLA_CEX_UUID env var")
	}

	ignition := conf.Ignition(`{
		"ignition": {
			"version": "3.5.0"
		},
		"kernelArguments": {
			"shouldExist": [
				"rd.luks.key=/etc/luks/cex.key"
		]
		},
		"storage": {
			"luks": [
				{
					"name": "root",
					"device": "/dev/disk/by-label/root",
					"cex": {
						"enabled": true
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
	}`)

	opts := platform.QemuMachineOptions{
		Cex: true,
	}
	opts.MinMemory = 8192

	switch pc := c.Cluster.(type) {
	case *qemu.Cluster:
		m, err = pc.NewMachineWithQemuOptions(ignition, opts)
	default:
		panic("Unsupported cluster type")
	}

	// copy over kolet into the machine
	if err := kola.ScpKolet([]platform.Machine{m}); err != nil {
		c.Fatal(err)
	}
	coretest.LocalTests(c)

	if err != nil {
		c.Fatalf("Unable to create test machine: %v", err)
	}
	rootPart := "/dev/disk/by-partlabel/root"

	ut.LUKSSanityCEXTest(c, m, rootPart)
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
