package rhcos

import (
	"regexp"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform/conf"
)

func init() {
	register.RegisterTest(&register.Test{
		Run:                  luksTPMTest,
		ClusterSize:          1,
		Name:                 `rhcos.luks.tpm`,
		Flags:                []register.Flag{},
		Distros:              []string{"rhcos"},
		Platforms:            []string{"qemu-unpriv"},
		ExcludeArchitectures: []string{"s390x", "ppc64le"}, // no TPM support for s390x, ppc64le in qemu
		UserData: conf.Ignition(`{
			"ignition": {
				"version": "2.2.0"
			},
			"storage": {
				"files": [
					{
						"filesystem": "root",
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
}

func mustMatch(c cluster.TestCluster, r string, output []byte) {
	m, err := regexp.Match(r, output)
	if err != nil {
		c.Fatal("Failed to match regexp %s: %v", r, err)
	}
	if !m {
		c.Fatal("Regexp %s did not match text: %s", r, output)
	}
}

func mustNotMatch(c cluster.TestCluster, r string, output []byte) {
	m, err := regexp.Match(r, output)
	if err != nil {
		c.Fatal("Failed to match regexp %s: %v", r, err)
	}
	if m {
		c.Fatal("Regexp %s matched text: %s", r, output)
	}
}

// Verify that the rootfs is encrypted with the TPM
func luksTPMTest(c cluster.TestCluster) {
	m := c.Machines()[0]
	luksDump := c.MustSSH(m, "sudo cryptsetup luksDump /dev/disk/by-partlabel/luks_root")
	// Yes, some hacky regexps.  There is luksDump --debug-json but we'd have to massage the JSON
	// out of other debug output and it's not clear to me it's going to be more stable.
	// We're just going for a basic sanity check here.
	mustMatch(c, "Cipher: *aes", luksDump)
	mustNotMatch(c, "Cipher: *cipher_null-ecb", luksDump)
	mustMatch(c, "0: *clevis", luksDump)
	mustNotMatch(c, "9: *coreos", luksDump)
	journalDump := c.MustSSH(m, "sudo journalctl -q -b -u coreos-encrypt --grep=pin=tpm2")
	mustMatch(c, "pin=tpm2", journalDump)
	// And validate we can automatically unlock it on reboot
	m.Reboot()
	luksDump = c.MustSSH(m, "sudo cryptsetup luksDump /dev/disk/by-partlabel/luks_root")
	mustMatch(c, "Cipher: *aes", luksDump)
}
