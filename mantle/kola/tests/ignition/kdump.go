package ignition

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/coreos/coreos-assembler/mantle/kola"
	"github.com/coreos/coreos-assembler/mantle/kola/cluster"
	"github.com/coreos/coreos-assembler/mantle/kola/register"
	"github.com/coreos/coreos-assembler/mantle/platform"
	"github.com/coreos/coreos-assembler/mantle/platform/conf"
	"github.com/coreos/coreos-assembler/mantle/platform/machine/qemu"
	"github.com/coreos/coreos-assembler/mantle/util"
)

// Test kdump to remote hosts

func init() {
	// Create 0 cluster size to allow starting and setup ssh server as needed for the test
	// See: https://github.com/coreos/coreos-assembler/pull/1310#discussion_r401908836
	register.RegisterTest(&register.Test{
		Run:         kdumpSSHTest,
		ClusterSize: 0,
		Name:        `kdump.crash.ssh`,
		Description: "Verifies kdump logs are exported to SSH destination",
		Tags:        []string{"kdump", kola.SkipBaseChecksTag, kola.NeedsInternetTag},
		Platforms:   []string{"qemu"},
	})
}

// The destination VM for kdump logs
type SshServer struct {
	Machine        platform.Machine
	MachineAddress string
	SSHPort        string
	PrivSSH        string
	PubSSH         string
}

// Start a VM and return the SSH key pair.
func setupSSHMachine(c cluster.TestCluster) SshServer {
	var m platform.Machine
	var err error
	var address string
	var port string

	options := platform.QemuMachineOptions{
		HostForwardPorts: []platform.HostForwardPort{
			{Service: "ssh", HostPort: 0, GuestPort: 22},
		},
	}

	// temp dir to store SSH keys
	tmpd, err := os.MkdirTemp("", "kola-kdump-crash-ssh")
	if err != nil {
		c.Fatalf("Error creating tempdir: %v", err)
	}
	defer os.RemoveAll(tmpd)

	// generate an ssh key pair we'll use for authentication
	pubkeyBuf, privkeyPath, err := util.CreateSSHAuthorizedKey(tmpd)
	if err != nil {
		c.Fatalf("Error creating ssh keys: %v", err)
	}

	// load the private key as well
	privKeyBuf, err := os.ReadFile(privkeyPath)
	if err != nil {
		c.Fatalf("error reading pubkey: %v", err)
	}

	// Inject the public key previously created as an
	// authorized key
	ignition := conf.Ignition(fmt.Sprintf(`{
       "ignition": { "version": "3.4.0" },
       "passwd":{
         "users":[
           {
             "name":"core",
             "sshAuthorizedKeys": ["%s"]
            }
          ]
       }
    }`, strings.TrimSpace(string(pubkeyBuf))))

	// start the machine
	switch c := c.Cluster.(type) {
	// These cases have to be separated because when put together to the same case statement
	// the golang compiler no longer checks that the individual types in the case have the
	// NewMachineWithQemuOptions function, but rather whether platform.Cluster
	// does which fails
	case *qemu.Cluster:
		m, err = c.NewMachineWithQemuOptions(ignition, options)
	default:
		panic("unreachable")
	}
	if err != nil {
		c.Fatal(err)
	}

	// get the ssh port
	for _, hfp := range options.HostForwardPorts {
		if hfp.Service == "ssh" {
			address = "10.0.2.2"
			port = fmt.Sprintf("%d", hfp.HostPort)
		}
	}

	return SshServer{
		Machine:        m,
		MachineAddress: address,
		SSHPort:        port,
		PubSSH:         string(pubkeyBuf),
		PrivSSH:        string(privKeyBuf),
	}
}

func kdumpSSHTest(c cluster.TestCluster) {
	ssh_host := setupSSHMachine(c)

	// insert indentation in front SSH prviate key lines
	// to avoid errors in the butane file
	var padded = ""
	for _, line := range strings.Split(strings.TrimSuffix(ssh_host.PrivSSH, "\n"), "\n") {
		padded = fmt.Sprintf("%s          %s\n", padded, line)
	}

	butane := conf.Butane(fmt.Sprintf(`variant: fcos
version: 1.5.0
storage:
  files:
    - path: /root/.ssh/id_ssh_kdump.pub
      mode: 0600
      contents:
        inline: |
          %s
    - path: /root/.ssh/id_ssh_kdump
      mode: 0600
      contents:
        inline: |
%s
    - path: /root/.ssh/config
      mode: 0644
      overwrite: true
      contents:
        inline: |
           Host %s
               StrictHostKeyChecking no
               Port %s
    - path: /etc/kdump.conf
      overwrite: true
      contents:
        inline: |
          ssh core@%s
          sshkey /root/.ssh/id_ssh_kdump
          path /home/core/crash
          core_collector makedumpfile -F -l --message-level 1 -d 31
systemd:
  units:
    - name: kdump.service
      enabled: true
      dropins:
        - name: debug.conf
          contents: |
            [Service]
            Environment="debug=1"
kernel_arguments:
    should_exist:
      - crashkernel=512M`,
		ssh_host.PubSSH, padded, ssh_host.MachineAddress, ssh_host.SSHPort, ssh_host.MachineAddress))

	opts := platform.MachineOptions{
		MinMemory: 2048,
	}

	kdump_machine, err := c.NewMachineWithOptions(butane, opts)
	if err != nil {
		c.Fatalf("Unable to create test machine: %v", err)
	}

	// Wait for kdump to become active
	// 3 minutes should be enough to generate the kdump initramfs
	err = util.Retry(12, 15*time.Second, func() error {

		kdump_status, err := c.SSH(kdump_machine, "systemctl is-active kdump.service")

		if err != nil {
			return err
		} else if string(kdump_status) == "inactive" {
			return fmt.Errorf("kdump.service is not ready: %s", string(kdump_status))
		}
		return nil
	})
	if err != nil {
		c.Fatalf("Timed out while waiting for kdump.service to be ready: %v", err)
	}

	// crash the kernel
	// use systemd-run because  direclty calling `echo c...` will alaways
	// throw an error as  the kernel immediately hangs.
	_, err = c.SSH(kdump_machine, "sudo systemd-run sh -c 'sleep 5 && echo c > /proc/sysrq-trigger'")
	if err != nil {
		c.Fatalf("failed to queue kernel crash: %v", err)
	}

	// Wait for kdump to create vmcore dump on the remote host
	err = util.Retry(5, 10*time.Second, func() error {

		// Look for the crash files created on the SSH machine
		logs, err := c.SSH(ssh_host.Machine, "find /home/core/crash -type f -name vmcore*")

		if err != nil {
			return fmt.Errorf("failed to search for vmcore: %w", err)
		} else if logs == nil {
			return fmt.Errorf("No vmcore created on remote SSH host")
		}
		return nil
	})
	if err != nil {
		c.Fatalf("Timed out while waiting for kdump to create vmcore files: %v", err)
	}
}
