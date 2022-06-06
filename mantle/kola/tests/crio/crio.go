// Copyright 2018 Red Hat, Inc.
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

package crio

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/net/context"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/kola/tests/util"
	"github.com/coreos/mantle/lang/worker"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/conf"
)

// simplifiedCrioInfo represents the results from crio info
type simplifiedCrioInfo struct {
	StorageDriver string `json:"storage_driver"`
	StorageRoot   string `json:"storage_root"`
	CgroupDriver  string `json:"cgroup_driver"`
}

// overrideCrioOperationTimeoutSeconds replaces the currently *extremely* low
// default crio operation timeouts that cause flakes in our CI.
// See https://github.com/openshift/os/issues/818
const overrideCrioOperationTimeoutSeconds = "300s"

// crioPodTemplate is a simple string template required for creating a pod in crio
// It takes two strings: the name (which will be expanded) and the generated image name
var crioPodTemplate = `{
	"metadata": {
		"name": "rhcos-crio-pod-%s",
		"namespace": "redhat.test.crio"
	},
	"image": {
			"image": "localhost/%s:latest"
	},
	"args": [],
	"readonly_rootfs": false,
	"log_path": "",
	"stdin": false,
	"stdin_once": false,
	"tty": false,
	"linux": {
			"resources": {
					"memory_limit_in_bytes": 209715200,
					"cpu_period": 10000,
					"cpu_quota": 20000,
					"cpu_shares": 512,
					"oom_score_adj": 30,
					"cpuset_cpus": "0",
					"cpuset_mems": "0"
			},
			"cgroup_parent": "Burstable-pod-123.slice",
			"security_context": {
					"namespace_options": {
							"pid": 1
					},
					"capabilities": {
							"add_capabilities": [
								"sys_admin"
							]
					}
			}
	}
}`

// crioContainerTemplate is a simple string template required for running a container
// It takes three strings: the name (which will be expanded), the image, and the argument to run.
// For mounts see: https://godoc.org/k8s.io/cri-api/pkg/apis/runtime/v1alpha2#Mount
var crioContainerTemplate = `{
	"metadata": {
		"name": "rhcos-crio-container-%s",
		"attempt": 1
	},
	"image": {
		"image": "localhost/%s:latest"
	},
	"command": [
		"%s"
	],
	"args": [],
	"working_dir": "/",
	"envs": [
		{
			"key": "PATH",
			"value": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
		},
		{
			"key": "TERM",
			"value": "xterm"
		}
	],
	"labels": {
		"type": "small",
		"batch": "no"
	},
	"annotations": {
		"daemon": "crio"
	},
	"privileged": true,
	"log_path": "",
	"stdin": false,
	"stdin_once": false,
	"tty": false,
	"linux": {
		"resources": {
			"cpu_period": 10000,
			"cpu_quota": 20000,
			"cpu_shares": 512,
			"oom_score_adj": 30,
			"memory_limit_in_bytes": 268435456
		},
		"security_context": {
			"namespace_options": {
				"pid": 1
			},
			"readonly_rootfs": false,
			"selinux_options": {
				"user": "system_u",
				"role": "system_r",
				"type": "svirt_lxc_net_t",
				"level": "s0:c4,c5"
			},
			"capabilities": {
				"add_capabilities": [
					"setuid",
					"setgid",
					"net_raw"
				],
				"drop_capabilities": [
				]
			}
		}
	},
	"mounts": [{
		"container_path": "/tmp/test",
		"host_path": "/tmp/test",
		"read_only": false,
		"selinux_relabel": true,
		"propagation": 2
	}]
}`

// RHCOS has the crio service disabled by default, so use Ignition to enable it
var enableCrioIgn = conf.Ignition(`{
  "ignition": {
    "version": "3.0.0"
  },
  "storage": {
	"directories": [{
		"path": "/tmp/test",
		"mode": 511
	}]
  },
  "systemd": {
    "units": [
      {
        "enabled": true,
        "name": "crio.service"
      }
    ]
  }
}`)

// init runs when the package is imported and takes care of registering tests
func init() {
	register.RegisterTest(&register.Test{
		Run:         crioBaseTests,
		ClusterSize: 1,
		Name:        `crio.base`,
		// crio pods require fetching a kubernetes pause image
		Flags:    []register.Flag{register.RequiresInternetAccess},
		Distros:  []string{"rhcos"},
		UserData: enableCrioIgn,
		Tags:     []string{"crio"},
	})
	register.RegisterTest(&register.Test{
		Run:         crioNetwork,
		ClusterSize: 2,
		Name:        "crio.network",
		Flags:       []register.Flag{register.RequiresInternetAccess},
		Distros:     []string{"rhcos"},
		UserData:    enableCrioIgn,
		Tags:        []string{"crio"},
		// qemu-unpriv machines cannot communicate between each other
		ExcludePlatforms: []string{"qemu-unpriv"},
	})
}

// crioBaseTests executes multiple tests under the "base" name
func crioBaseTests(c cluster.TestCluster) {
	c.Run("crio-info", testCrioInfo)
	c.Run("pod-continues-during-service-restart", crioPodContinuesDuringServiceRestart)
	c.Run("networks-reliably", crioNetworksReliably)
}

// generateCrioConfig generates a crio pod/container configuration
// based on the input name and arguments returning the path to the generated configs.
func generateCrioConfig(podName, imageName string, command []string) (string, string, error) {
	fileContentsPod := fmt.Sprintf(crioPodTemplate, podName, imageName)

	tmpFilePod, err := ioutil.TempFile("", podName+"Pod")
	if err != nil {
		return "", "", err
	}
	defer tmpFilePod.Close()
	if _, err = tmpFilePod.Write([]byte(fileContentsPod)); err != nil {
		return "", "", err
	}
	cmd := strings.Join(command, " ")
	fileContentsContainer := fmt.Sprintf(crioContainerTemplate, imageName, imageName, cmd)

	tmpFileContainer, err := ioutil.TempFile("", imageName+"Container")
	if err != nil {
		return "", "", err
	}
	defer tmpFileContainer.Close()
	if _, err = tmpFileContainer.Write([]byte(fileContentsContainer)); err != nil {
		return "", "", err
	}

	return tmpFilePod.Name(), tmpFileContainer.Name(), nil
}

// genContainer makes a container out of binaries on the host. This function uses podman to build.
// The first string returned by this function is the pod config to be used with crictl runp. The second
// string returned is the container config to be used with crictl create/exec. They will be dropped
// on to all machines in the cluster as ~/$STRING_RETURNED_FROM_FUNCTION. Note that the string returned
// here is just the name, not the full path on the cluster machine(s).
func genContainer(c cluster.TestCluster, m platform.Machine, podName, imageName string, binnames []string, shellCommands []string) (string, string, error) {
	configPathPod, configPathContainer, err := generateCrioConfig(podName, imageName, shellCommands)
	if err != nil {
		return "", "", err
	}
	if err = cluster.DropFile(c.Machines(), configPathPod); err != nil {
		return "", "", err
	}
	if err = cluster.DropFile(c.Machines(), configPathContainer); err != nil {
		return "", "", err
	}
	// Create the crio image used for testing, only if it doesn't exist already
	output := c.MustSSH(m, "sudo podman images -n --format '{{.Repository}}'")
	if !strings.Contains(string(output), "localhost/"+imageName) {
		util.GenPodmanScratchContainer(c, m, imageName, binnames)
	}

	return path.Base(configPathPod), path.Base(configPathContainer), nil
}

// crioNetwork ensures that crio containers can make network connections outside of the host
func crioNetwork(c cluster.TestCluster) {
	machines := c.Machines()
	src, dest := machines[0], machines[1]

	c.Log("creating ncat containers")

	// Since genContainer also generates crio pod/container configs,
	// there will be a duplicate config file on each machine.
	// Thus we only save one set for later use.
	crioConfigPod, crioConfigContainer, err := genContainer(c, src, "ncat", "ncat", []string{"ncat", "echo"}, []string{"ncat"})
	if err != nil {
		c.Fatal(err)
	}
	_, _, err = genContainer(c, dest, "ncat", "ncat", []string{"ncat", "echo"}, []string{"ncat"})
	if err != nil {
		c.Fatal(err)
	}

	listener := func(ctx context.Context) error {
		podID, err := c.SSHf(dest, "sudo crictl runp -T %s %s", overrideCrioOperationTimeoutSeconds, crioConfigPod)
		if err != nil {
			return err
		}

		containerID, err := c.SSHf(dest, "sudo crictl create -T %s --no-pull %s %s %s",
			overrideCrioOperationTimeoutSeconds,
			podID, crioConfigContainer, crioConfigPod)
		if err != nil {
			return err
		}

		// This command will block until a message is recieved
		output, err := c.SSHf(dest, "sudo timeout 30 crictl exec %s echo 'HELLO FROM SERVER' | timeout 20 ncat --listen 0.0.0.0 9988 || echo 'LISTENER TIMEOUT'", containerID)
		if err != nil {
			return err
		}
		if string(output) != "HELLO FROM CLIENT" {
			return fmt.Errorf("unexpected result from listener: %s", output)
		}

		return nil
	}

	talker := func(ctx context.Context) error {
		// Wait until listener is ready before trying anything
		for {
			_, err := c.SSH(dest, "sudo ss -tulpn|grep 9988")
			if err == nil {
				break // socket is ready
			}

			exit, ok := err.(*ssh.ExitError)
			if !ok || exit.Waitmsg.ExitStatus() != 1 { // 1 is the expected exit of grep -q
				return err
			}

			select {
			case <-ctx.Done():
				return fmt.Errorf("timeout waiting for server")
			default:
				time.Sleep(100 * time.Millisecond)
			}
		}
		podID, err := c.SSHf(src, "sudo crictl runp -T %s %s", overrideCrioOperationTimeoutSeconds, crioConfigPod)
		if err != nil {
			return err
		}

		containerID, err := c.SSHf(src, "sudo crictl create -T %s --no-pull %s %s %s",
			overrideCrioOperationTimeoutSeconds,
			podID, crioConfigContainer, crioConfigPod)
		if err != nil {
			return err
		}

		output, err := c.SSHf(src, "sudo crictl exec %s echo 'HELLO FROM CLIENT' | ncat %s 9988",
			containerID, dest.PrivateIP())
		if err != nil {
			return err
		}
		if string(output) != "HELLO FROM SERVER" {
			return fmt.Errorf(`unexpected result from listener: "%s"`, output)
		}

		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	if err := worker.Parallel(ctx, listener, talker); err != nil {
		c.Fatal(err)
	}
}

// crioNetworksReliably verifies that crio containers have a reliable network
func crioNetworksReliably(c cluster.TestCluster) {
	m := c.Machines()[0]

	// Figure out the host IP address on the crio default bridge. This is
	// required as the default subnet was changed in 1.18 to avoid a conflict
	// with the default podman bridge.
	subnet := c.MustSSH(m, "jq --raw-output '.ipam.ranges[0][0].subnet' /usr/etc/cni/net.d/100-crio-bridge.conf")
	hostIP := fmt.Sprintf("%s.1", strings.TrimSuffix(string(subnet), ".0/16"))

	// Here we generate 10 pods, each will run a container responsible for
	// pinging to host
	output := ""
	for x := 1; x <= 10; x++ {
		// append int to name to avoid pod name collision
		crioConfigPod, crioConfigContainer, err := genContainer(
			c, m, fmt.Sprintf("ping%d", x), "ping", []string{"ping"},
			[]string{"ping"})
		if err != nil {
			c.Fatal(err)
		}

		cmdCreatePod := fmt.Sprintf("sudo crictl runp -T %s %s", overrideCrioOperationTimeoutSeconds, crioConfigPod)
		podID := c.MustSSH(m, cmdCreatePod)
		containerID := c.MustSSH(m, fmt.Sprintf("sudo crictl create -T %s --no-pull %s %s %s",
			overrideCrioOperationTimeoutSeconds,
			podID, crioConfigContainer, crioConfigPod))
		output = output + string(c.MustSSH(m, fmt.Sprintf("sudo crictl exec %s ping -i 0.2 %s -w 1 >/dev/null && echo PASS || echo FAIL", containerID, hostIP)))
	}

	numPass := strings.Count(string(output), "PASS")
	if numPass != 10 {
		c.Fatalf("Expected 10 passes, but received %d passes with output: %s", numPass, output)
	}

}

// getCrioInfo parses and returns the information crio provides via socket
func getCrioInfo(c cluster.TestCluster, m platform.Machine) (simplifiedCrioInfo, error) {
	target := simplifiedCrioInfo{}
	crioInfoJSON, err := c.SSH(m, `sudo curl -s --unix-socket /var/run/crio/crio.sock http://crio/info`)
	if err != nil {
		return target, fmt.Errorf("could not get info: %v", err)
	}

	err = json.Unmarshal(crioInfoJSON, &target)
	if err != nil {
		return target, fmt.Errorf("could not unmarshal info %q into known json: %v", string(crioInfoJSON), err)
	}
	return target, nil
}

// testCrioInfo test that crio info's output is as expected.
func testCrioInfo(c cluster.TestCluster) {
	m := c.Machines()[0]
	info, err := getCrioInfo(c, m)
	if err != nil {
		c.Fatal(err)
	}
	expectedStorageDriver := "overlay"
	if info.StorageDriver != expectedStorageDriver {
		c.Errorf("unexpected storage driver: %v != %v", expectedStorageDriver, info.StorageDriver)
	}
	expectedStorageRoot := "/var/lib/containers/storage"
	if info.StorageRoot != expectedStorageRoot {
		c.Errorf("unexpected storage root: %v != %v", expectedStorageRoot, info.StorageRoot)
	}
	expectedCgroupDriver := "systemd"
	if info.CgroupDriver != expectedCgroupDriver {
		c.Errorf("unexpected cgroup driver: %v != %v", expectedCgroupDriver, info.CgroupDriver)
	}

}

// crioPodContinuesDuringServiceRestart verifies that a crio pod does not
// stop when the service is restarted
func crioPodContinuesDuringServiceRestart(c cluster.TestCluster) {
	m := c.Machines()[0]

	crioConfigPod, crioConfigContainer, err := genContainer(
		c, m, "restart-test", "sleep",
		[]string{"bash", "sleep", "echo"}, []string{"bash"})
	if err != nil {
		c.Fatal(err)
	}
	cmdCreatePod := fmt.Sprintf("sudo crictl runp -T %s %s", overrideCrioOperationTimeoutSeconds, crioConfigPod)
	podID := c.MustSSH(m, cmdCreatePod)
	containerID := c.MustSSH(m, fmt.Sprintf("sudo crictl create -T %s --no-pull %s %s %s",
		overrideCrioOperationTimeoutSeconds,
		podID, crioConfigContainer, crioConfigPod))

	cmd := fmt.Sprintf("sudo crictl exec %s bash -c \"sleep 25 && echo PASS > /tmp/test/restart-test\"", containerID)
	c.RunCmdSync(m, cmd)
	time.Sleep(3 * time.Second)
	c.RunCmdSync(m, "sudo systemctl restart crio")
	time.Sleep(25 * time.Second)
	output := strings.TrimSuffix(string(c.MustSSH(m, "cat /tmp/test/restart-test")), "\n")

	if output != "PASS" {
		c.Fatalf("Pod did not continue during service restart. Output=%s, Command=%s", output, cmd)
	}
}
