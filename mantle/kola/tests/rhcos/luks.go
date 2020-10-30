package rhcos

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"time"

	"github.com/coreos/mantle/kola"
	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/conf"
	"github.com/coreos/mantle/platform/machine/unprivqemu"
	"github.com/coreos/mantle/util"
)

func init() {
	register.RegisterTest(&register.Test{
		Run:                  luksTPMTest,
		ClusterSize:          1,
		Name:                 `rhcos.luks.tpm`,
		Flags:                []register.Flag{},
		Distros:              []string{"rhcos"},
		Platforms:            []string{"qemu-unpriv"},
		ExcludeArchitectures: []string{"s390x", "ppc64le", "aarch64"}, // no TPM support for s390x, ppc64le, aarch64 in qemu
		Tags:                 []string{"luks", "tpm"},
		UserDataV3: conf.Ignition(`{
			"ignition": {
				"version": "3.0.0"
			},
			"storage": {
				"files": [
					{
						"path": "/etc/clevis.json",
						"contents": {
							"source": "data:text/plain;base64,e30K"
						},
						"mode": 420
					}
				]
			}
		}`),
	})
	// Create 0 cluster size to allow starting and setup of Tang as needed per test
	// See: https://github.com/coreos/coreos-assembler/pull/1310#discussion_r401908836
	register.RegisterTest(&register.Test{
		Run:                  luksTangTest,
		ClusterSize:          0,
		Name:                 `rhcos.luks.tang`,
		Flags:                []register.Flag{},
		Distros:              []string{"rhcos"},
		ExcludeArchitectures: []string{"s390x", "ppc64le", "aarch64"}, // no TPM support for s390x, ppc64le, aarch64 in qemu
		Tags:                 []string{"luks", "tang", kola.NeedsInternetTag},
	})
	register.RegisterTest(&register.Test{
		Run:                  luksSSST1Test,
		ClusterSize:          0,
		Name:                 `rhcos.luks.sss.t1`,
		Flags:                []register.Flag{},
		Distros:              []string{"rhcos"},
		Platforms:            []string{"qemu-unpriv"},
		ExcludeArchitectures: []string{"s390x", "ppc64le", "aarch64"}, // no TPM support for s390x, ppc64le, aarch64 in qemu
		Tags:                 []string{"luks", "tpm", "tang", "sss", kola.NeedsInternetTag},
	})
	register.RegisterTest(&register.Test{
		Run:                  luksSSST2Test,
		ClusterSize:          0,
		Name:                 `rhcos.luks.sss.t2`,
		Flags:                []register.Flag{},
		Distros:              []string{"rhcos"},
		Platforms:            []string{"qemu-unpriv"},
		ExcludeArchitectures: []string{"s390x", "ppc64le", "aarch64"}, // no TPM support for s390x, ppc64le, aarch64 in qemu
		Tags:                 []string{"luks", "tpm", "tang", "sss", kola.NeedsInternetTag},
	})
}

func setupTangMachine(c cluster.TestCluster) (string, string) {
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
	containerID, errMsg, err := m.SSH("sudo podman run -d -p 80:80 quay.io/mike_nguyen/tang")
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

	return tangAddress, string(thumbprint)
}

func getEncodedTangPin(c cluster.TestCluster, address string, thumbprint string) string {
	return b64Encode(getTangPin(c, address, thumbprint))
}

func getTangPin(c cluster.TestCluster, address string, thumbprint string) string {
	return fmt.Sprintf(`{
	"url": "http://%s",
	"thp": "%s"
}`, address, string(thumbprint))
}

// Generates a SSS clevis pin with TPM2 and a valid/invalid Tang config
func getEncodedSSSPin(c cluster.TestCluster, num int, tang bool, address string, thumbprint string) string {
	tangPin := getTangPin(c, address, thumbprint)
	if !tang {
		tangPin = fmt.Sprintf(`{
	"url": "http://%s",
	"thp": "INVALIDTHUMBPRINT"
}`, address)
	}

	return b64Encode(fmt.Sprintf(`{
	"t": %d,
	"pins": {
		"tpm2": {},
		"tang": %s
	}
}`, num, tangPin))
}

func b64Encode(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

func mustMatch(c cluster.TestCluster, r string, output []byte) {
	m, err := regexp.Match(r, output)
	if err != nil {
		c.Fatalf("Failed to match regexp %s: %v", r, err)
	}
	if !m {
		c.Fatalf("Regexp %s did not match text: %s", r, output)
	}
}

func mustNotMatch(c cluster.TestCluster, r string, output []byte) {
	m, err := regexp.Match(r, output)
	if err != nil {
		c.Fatalf("Failed to match regexp %s: %v", r, err)
	}
	if m {
		c.Fatalf("Regexp %s matched text: %s", r, output)
	}
}

func luksSanityTest(c cluster.TestCluster, m platform.Machine, pin string) {
	luksDump := c.MustSSH(m, "sudo cryptsetup luksDump /dev/disk/by-partlabel/luks_root")
	// Yes, some hacky regexps.  There is luksDump --debug-json but we'd have to massage the JSON
	// out of other debug output and it's not clear to me it's going to be more stable.
	// We're just going for a basic sanity check here.
	mustMatch(c, "Cipher: *aes", luksDump)
	mustNotMatch(c, "Cipher: *cipher_null-ecb", luksDump)
	mustMatch(c, "0: *clevis", luksDump)
	mustNotMatch(c, "9: *coreos", luksDump)
	journalDump := c.MustSSH(m, fmt.Sprintf("sudo journalctl -q -b -u coreos-encrypt --grep=pin=%s", pin))
	mustMatch(c, fmt.Sprintf("pin=%s", pin), journalDump)
	if pin == "sss" || pin == "tang" {
		c.MustSSH(m, "sudo rpm-ostree kargs --append ip=dhcp --append rd.neednet=1")
	}
	// And validate we can automatically unlock it on reboot
	err := m.Reboot()
	if err != nil {
		c.Fatalf("Failed to reboot the machine: %v", err)
	}
	luksDump = c.MustSSH(m, "sudo cryptsetup luksDump /dev/disk/by-partlabel/luks_root")
	mustMatch(c, "Cipher: *aes", luksDump)
}

// Verify that the rootfs is encrypted with the TPM
func luksTPMTest(c cluster.TestCluster) {
	luksSanityTest(c, c.Machines()[0], "tpm2")
}

// Verify that the rootfs is encrypted with Tang
func luksTangTest(c cluster.TestCluster) {
	address, thumbprint := setupTangMachine(c)
	encodedTangPin := getEncodedTangPin(c, address, thumbprint)

	ignition := conf.Ignition(fmt.Sprintf(`{
		"ignition": {
			"version": "3.0.0"
		},
		"storage": {
			"files": [
				{
					"path": "/etc/clevis.json",
					"contents": {
						"source": "data:text/plain;base64,%s"
					},
					"mode": 420
				}
			]
		}
	}`, encodedTangPin))

	m, err := c.NewMachine(ignition)
	if err != nil {
		c.Fatalf("Unable to create test machine: %v", err)
	}
	luksSanityTest(c, m, "tang")
}

// Verify that the rootfs is encrypted with SSS with t=1
func luksSSST1Test(c cluster.TestCluster) {
	address, thumbprint := setupTangMachine(c)
	encodedSSST1Pin := getEncodedSSSPin(c, 1, false, address, thumbprint)

	ignition := conf.Ignition(fmt.Sprintf(`{
		"ignition": {
			"version": "3.0.0"
		},
		"storage": {
			"files": [
				{
					"filesystem": "root",
					"path": "/etc/clevis.json",
					"contents": {
						"source": "data:text/plain;base64,%s"
					},
					"mode": 420
				}
			]
		}
	}`, encodedSSST1Pin))

	m, err := c.NewMachine(ignition)
	if err != nil {
		c.Fatalf("Unable to create test machine: %v", err)
	}
	luksSanityTest(c, m, "sss")
}

// Verify that the rootfs is encrypted with SSS with t=2
func luksSSST2Test(c cluster.TestCluster) {
	address, thumbprint := setupTangMachine(c)
	encodedSSST2Pin := getEncodedSSSPin(c, 2, true, address, thumbprint)

	ignition := conf.Ignition(fmt.Sprintf(`{
		"ignition": {
			"version": "3.0.0"
		},
		"storage": {
			"files": [
				{
					"filesystem": "root",
					"path": "/etc/clevis.json",
					"contents": {
						"source": "data:text/plain;base64,%s"
					},
					"mode": 420
				}
			]
		}
	}`, encodedSSST2Pin))

	m, err := c.NewMachine(ignition)
	if err != nil {
		c.Fatalf("Unable to create test machine: %v", err)
	}
	luksSanityTest(c, m, "sss")
}
