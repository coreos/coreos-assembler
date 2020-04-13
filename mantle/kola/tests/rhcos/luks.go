package rhcos

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"os/user"
	"regexp"
	"strconv"
	"sync"
	"syscall"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform/conf"
)

var runOnce sync.Once

const (
	dbDir          = "/tmp/tang/db"
	cacheDir       = "/tmp/tang/cache"
	xinetdConfFile = "/tmp/tang/xinetd.conf"
	xinetdPidFile  = "/tmp/tang/pid"
	tangLogFile    = "/tmp/tang/tang.log"
	xinetdLogFile  = "/tmp/tang/xinetd.log"
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
	// Create 0 cluster size to allow starting and setup of Tang as needed per test
	// See: https://github.com/coreos/coreos-assembler/pull/1310#discussion_r401908836
	register.RegisterTest(&register.Test{
		Run:                  luksTangTest,
		ClusterSize:          0,
		Name:                 `rhcos.luks.tang`,
		Flags:                []register.Flag{},
		Distros:              []string{"rhcos"},
		Platforms:            []string{"qemu-unpriv"},
		ExcludeArchitectures: []string{"s390x", "ppc64le"}, // no TPM support for s390x, ppc64le in qemu
	})
	register.RegisterTest(&register.Test{
		Run:                  luksSSST1Test,
		ClusterSize:          0,
		Name:                 `rhcos.luks.sss.t1`,
		Flags:                []register.Flag{},
		Distros:              []string{"rhcos"},
		Platforms:            []string{"qemu-unpriv"},
		ExcludeArchitectures: []string{"s390x", "ppc64le"}, // no TPM support for s390x, ppc64le in qemu
	})
	register.RegisterTest(&register.Test{
		Run:                  luksSSST2Test,
		ClusterSize:          0,
		Name:                 `rhcos.luks.sss.t2`,
		Flags:                []register.Flag{},
		Distros:              []string{"rhcos"},
		Platforms:            []string{"qemu-unpriv"},
		ExcludeArchitectures: []string{"s390x", "ppc64le"}, // no TPM support for s390x, ppc64le in qemu
	})
}

func setupTangKeys(c cluster.TestCluster) {
	runOnce.Do(func() {
		user, err := user.Current()
		if err != nil {
			c.Fatalf("Unable to get current user: %v", err)
		}

		// xinetd requires the service to be in /etc/services which Tang is not
		// included.  Use the webcache service (port 8080) and run as the
		// current user so we can run unprivileged
		xinetdConf := fmt.Sprintf(`defaults
{
	log_type	= SYSLOG daemon info 
	log_on_failure	= HOST
	log_on_success	= PID HOST DURATION EXIT

	cps		= 50 10
	instances	= 50
	per_source	= 10

	v6only		= no
	bind		= 0.0.0.0
	groups		= yes
	umask		= 002
}

service webcache
{
	server_args = %s /dev/null
	server = /usr/libexec/tangdw
	socket_type = stream
	protocol = tcp
	only_from = 10.0.2.15 127.0.0.1
	user = %s
	wait = no
}`, cacheDir, user.Username)
		if err := os.MkdirAll(cacheDir, 0755); err != nil {
			c.Fatalf("Unable to create %s: %v", err)
		}

		f, err := os.Open(cacheDir)
		if err != nil {
			c.Fatalf("Unable to open %s: %v", cacheDir, err)
		}
		// This is a simple check that the directory is empty
		_, err = f.Readdir(1)
		if err == io.EOF {
			if err := os.MkdirAll(dbDir, 0755); err != nil {
				c.Fatalf("Unable to create %s: %v", err)
			}
			err := ioutil.WriteFile(xinetdConfFile, []byte(xinetdConf), 0644)
			if err != nil {
				c.Fatalf("Unable to write xinetd configuration file: %v", err)
			}
			keygen := exec.Command("/usr/libexec/tangd-keygen", dbDir)
			if err := keygen.Run(); err != nil {
				c.Fatalf("Unable to generate Tang keys: %v", err)
			}
			update := exec.Command("/usr/libexec/tangd-update", dbDir, cacheDir)
			if err := update.Run(); err != nil {
				c.Fatalf("Unable to update Tang DB: %v", err)
			}
		}
	})
}

func getEncodedTangPin(c cluster.TestCluster) string {
	return b64Encode(getTangPin(c))
}

func startTang(c cluster.TestCluster) int {
	cmd := exec.Command("/usr/sbin/xinetd", "-f", xinetdConfFile, "-pidfile", xinetdPidFile, "-filelog", xinetdLogFile)
	// Give the xinetd child process a separate process group ID so it can be
	// killed independently from the test
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Run(); err != nil {
		c.Fatalf("Unable to start Tang: %v ", err)
	}

	// xinetd detatches itself and os.Process.Pid reports incorrect pid
	// Use pid file instead
	output, err := ioutil.ReadFile(xinetdPidFile)
	if err != nil {
		c.Fatalf("Unable to get pid %v, err")
	}

	p := bytes.Trim(output, "\n")
	pid, err := strconv.Atoi(string(p))
	if err != nil {
		c.Fatalf("Unable to convert pid to integer: %v", err)
	}

	return pid
}

func stopTang(c cluster.TestCluster, pid int) {
	// kill with a negative pid is killing the process group
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		c.Fatalf("Unable to stop xinetd: %v", err)
	}
}

func getTangPin(c cluster.TestCluster) string {
	tangThumbprint, err := exec.Command("tang-show-keys", "8080").Output()
	if err != nil {
		c.Fatalf("Unable to retrieve Tang thumbprint: %v", err)
	}

	return fmt.Sprintf(`{
	"url": "http://10.0.2.2:8080",
	"thp": "%s"
}`, tangThumbprint)
}

// Generates a SSS clevis pin with TPM2 and a valid/invalid Tang config
func getEncodedSSSPin(c cluster.TestCluster, num int, tang bool) string {
	tangPin := getTangPin(c)
	if !tang {
		tangPin = fmt.Sprintf(`{
	"url": "http://10.0.2.2:8080",
	"thp": "INVALIDTHUMBPRINT"
}`)
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

func luksSanityTest(c cluster.TestCluster, pin string) {
	m := c.Machines()[0]
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
	m.Reboot()
	luksDump = c.MustSSH(m, "sudo cryptsetup luksDump /dev/disk/by-partlabel/luks_root")
	mustMatch(c, "Cipher: *aes", luksDump)
}

// Verify that the rootfs is encrypted with the TPM
func luksTPMTest(c cluster.TestCluster) {
	luksSanityTest(c, "tpm2")
}

// Verify that the rootfs is encrypted with Tang
func luksTangTest(c cluster.TestCluster) {
	setupTangKeys(c)
	pid := startTang(c)
	defer stopTang(c, pid)
	encodedTangPin := getEncodedTangPin(c)
	c.NewMachine(conf.Ignition(fmt.Sprintf(`{
		"ignition": {
			"version": "2.2.0"
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
	}`, encodedTangPin)))
	luksSanityTest(c, "tang")

}

// Verify that the rootfs is encrypted with SSS with t=1
func luksSSST1Test(c cluster.TestCluster) {
	setupTangKeys(c)
	pid := startTang(c)
	defer stopTang(c, pid)
	encodedSSST1Pin := getEncodedSSSPin(c, 1, false)
	c.NewMachine(conf.Ignition(fmt.Sprintf(`{
		"ignition": {
			"version": "2.2.0"
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
	}`, encodedSSST1Pin)))
	luksSanityTest(c, "sss")
}

// Verify that the rootfs is encrypted with SSS with t=2
func luksSSST2Test(c cluster.TestCluster) {
	setupTangKeys(c)
	pid := startTang(c)
	defer stopTang(c, pid)
	encodedSSST2Pin := getEncodedSSSPin(c, 2, true)
	c.NewMachine(conf.Ignition(fmt.Sprintf(`{
		"ignition": {
			"version": "2.2.0"
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
	}`, encodedSSST2Pin)))
	luksSanityTest(c, "sss")
}
