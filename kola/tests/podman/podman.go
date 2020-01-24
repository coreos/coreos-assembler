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

package podman

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/net/context"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	tutil "github.com/coreos/mantle/kola/tests/util"
	"github.com/coreos/mantle/lang/worker"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/util"
)

// init runs when the package is imported and takes care of registering tests
func init() {
	register.RegisterTest(&register.Test{
		Run:         podmanBaseTest,
		ClusterSize: 1,
		Name:        `podman.base`,
		Distros:     []string{"fcos", "rhcos"},
	})
	// These remaining tests use networking, and hence don't work reliably on RHCOS
	// right now due to due to https://bugzilla.redhat.com/show_bug.cgi?id=1757572
	register.RegisterTest(&register.Test{
		Run:         podmanWorkflow,
		ClusterSize: 1,
		Name:        `podman.workflow`,
		Flags:       []register.Flag{register.RequiresInternetAccess}, // For pulling nginx
		Distros:     []string{"fcos"},
		FailFast:    true,
	})
	register.RegisterTest(&register.Test{
		Run:         podmanNetworksReliably,
		ClusterSize: 1,
		Name:        `podman.network-single`,
		Distros:     []string{"fcos"},
	})
	// https://github.com/coreos/mantle/pull/1080
	// register.RegisterTest(&register.Test{
	// 	Run:         podmanNetworkTest,
	// 	ClusterSize: 2,
	// 	Name:        `podman.network-multi`,
	// 	Distros:     []string{"fcos"},
	// })
}

// simplifiedContainerPsInfo represents a container entry in podman ps -a
type simplifiedContainerPsInfo struct {
	ID     string `json:"id"`
	Image  string `json:"image"`
	Status string `json:"status"`
}

// simplifiedPsInfo represents the results of podman ps -a
type simplifiedPsInfo struct {
	containers []simplifiedContainerPsInfo
}

// simplifiedPodmanInfo represents the results of podman info
type simplifiedPodmanInfo struct {
	Store struct {
		GraphDriverName string `json:"GraphDriverName"`
		GraphRoot       string `json:"GraphRoot"`
	}
}

func getSimplifiedPsInfo(c cluster.TestCluster, m platform.Machine) (simplifiedPsInfo, error) {
	target := simplifiedPsInfo{}
	psJSON, err := c.SSH(m, `sudo podman ps -a --format json`)

	if err != nil {
		return target, fmt.Errorf("could not get info: %v", err)
	}

	err = json.Unmarshal(psJSON, &target.containers)

	if err != nil {
		return target, fmt.Errorf("could not unmarshal info %q into known json: %v", string(psJSON), err)
	}
	return target, nil
}

// Returns the result of podman info as a simplifiedPodmanInfo
func getPodmanInfo(c cluster.TestCluster, m platform.Machine) (simplifiedPodmanInfo, error) {
	target := simplifiedPodmanInfo{}

	pInfoJSON, err := c.SSH(m, `sudo podman info --format json`)
	if err != nil {
		return target, fmt.Errorf("Could not get info: %v", err)
	}

	err = json.Unmarshal(pInfoJSON, &target)
	if err != nil {
		return target, fmt.Errorf("Could not unmarshal info %q into known JSON: %v", string(pInfoJSON), err)
	}
	return target, nil
}

func podmanBaseTest(c cluster.TestCluster) {
	c.Run("info", podmanInfo)
	c.Run("resources", podmanResources)
}

// Test: Run basic podman commands
func podmanWorkflow(c cluster.TestCluster) {
	m := c.Machines()[0]

	// Test: Verify container can run with volume mount and port forwarding
	image := "docker.io/library/nginx"
	wwwRoot := "/usr/share/nginx/html"
	var id string

	c.Run("run", func(c cluster.TestCluster) {
		dir := c.MustSSH(m, `mktemp -d`)
		cmd := fmt.Sprintf("echo TEST PAGE > %s/index.html", string(dir))
		c.MustSSH(m, cmd)

		cmd = fmt.Sprintf("sudo podman run -d -p 80:80 -v %s/index.html:%s/index.html:z %s", string(dir), wwwRoot, image)
		out := c.MustSSH(m, cmd)
		id = string(out)[0:12]

		podIsRunning := func() error {
			b, err := c.SSH(m, `curl -f http://localhost 2>/dev/null`)
			if err != nil {
				return err
			}
			if !bytes.Contains(b, []byte("TEST PAGE")) {
				return fmt.Errorf("nginx pod is not running %s", b)
			}
			return nil
		}

		if err := util.Retry(6, 5*time.Second, podIsRunning); err != nil {
			c.Fatal("Pod is not running")
		}
	})

	// Test: Execute command in container
	c.Run("exec", func(c cluster.TestCluster) {
		cmd := fmt.Sprintf("sudo podman exec %s echo hello", id)
		out := c.MustSSH(m, cmd)

		if string(out) != "hello" {
			c.Fatal("Could not exec command in container")
		}
	})

	// Test: Stop container
	c.Run("stop", func(c cluster.TestCluster) {
		cmd := fmt.Sprintf("sudo podman stop %s", id)
		c.MustSSH(m, cmd)
		psInfo, err := getSimplifiedPsInfo(c, m)
		if err != nil {
			c.Fatal(err)
		}

		found := false
		for _, container := range psInfo.containers {
			if container.ID == id {
				found = true
				if !strings.Contains(strings.ToLower(container.Status), "exited") {
					c.Fatalf("Container %s was not stopped. Current status: %s", id, container.Status)
				}
			}
		}

		if found == false {
			c.Fatalf("Unable to find container %s in podman ps -a output", id)
		}
	})

	// Test: Remove container
	c.Run("remove", func(c cluster.TestCluster) {
		cmd := fmt.Sprintf("sudo podman rm %s", id)
		c.MustSSH(m, cmd)
		psInfo, err := getSimplifiedPsInfo(c, m)
		if err != nil {
			c.Fatal(err)
		}

		found := false
		for _, container := range psInfo.containers {
			if container.ID == id {
				found = true
			}
		}

		if found == true {
			c.Fatalf("Container %s should be removed. %v", id, psInfo.containers)
		}
	})

	// Test: Delete container
	c.Run("delete", func(c cluster.TestCluster) {
		cmd := fmt.Sprintf("sudo podman rmi %s", image)
		out := c.MustSSH(m, cmd)
		imageID := string(out)

		cmd = fmt.Sprintf("sudo podman images | grep %s", imageID)
		out, err := c.SSH(m, cmd)
		if err == nil {
			c.Fatalf("Image should be deleted but found %s", string(out))
		}
	})
}

// Test: Verify basic podman info information
func podmanInfo(c cluster.TestCluster) {
	m := c.Machines()[0]
	info, err := getPodmanInfo(c, m)
	if err != nil {
		c.Fatal(err)
	}

	// test for known settings
	expectedGraphDriver := "overlay"
	if info.Store.GraphDriverName != expectedGraphDriver {
		c.Errorf("Unexpected driver: %v != %v", expectedGraphDriver, info.Store.GraphDriverName)
	}
	expectedGraphRoot := "/var/lib/containers/storage"
	if info.Store.GraphRoot != expectedGraphRoot {
		c.Errorf("Unexected graph root: %v != %v", expectedGraphRoot, info.Store.GraphRoot)
	}
}

// Test: Run podman with various options
func podmanResources(c cluster.TestCluster) {
	m := c.Machines()[0]

	tutil.GenPodmanScratchContainer(c, m, "echo", []string{"echo"})

	podmanFmt := "sudo podman run --net=none --rm %s echo echo 1"

	pCmd := func(arg string) string {
		return fmt.Sprintf(podmanFmt, arg)
	}

	for _, podmanCmd := range []string{
		// must set memory when setting memory-swap
		// See https://github.com/opencontainers/runc/issues/1980 for
		// why we use 128m for memory
		pCmd("--memory=128m --memory-swap=128m"),
		pCmd("--memory-reservation=10m"),
		pCmd("--kernel-memory=10m"),
		pCmd("--cpu-shares=100"),
		pCmd("--cpu-period=1000"),
		pCmd("--cpuset-cpus=0"),
		pCmd("--cpuset-mems=0"),
		pCmd("--cpu-quota=1000"),
		pCmd("--blkio-weight=10"),
		pCmd("--memory=128m --oom-kill-disable=true"),
		pCmd("--memory-swappiness=50"),
		pCmd("--shm-size=1m"),
	} {
		cmd := podmanCmd
		output, err := c.SSH(m, cmd)
		if err != nil {
			c.Fatalf("Failed to run %q: output: %q status: %q", cmd, output, err)
		}
	}
}

// Test: Verify network connectivity from containers on two different machines
func podmanNetworkTest(c cluster.TestCluster) {
	machines := c.Machines()
	src, dest := machines[0], machines[1]

	c.Log("creating ncat containers")

	tutil.GenPodmanScratchContainer(c, src, "ncat", []string{"ncat"})
	tutil.GenPodmanScratchContainer(c, dest, "ncat", []string{"ncat"})

	listener := func(ctx context.Context) error {
		// Will block until a message is recieved
		out, err := c.SSH(dest,
			`echo "HELLO FROM SERVER" | sudo podman run -i -p 9988:9988 ncat ncat --idle-timeout 20 --listen 0.0.0.0 9988`,
		)
		if err != nil {
			return err
		}

		if !bytes.Equal(out, []byte("HELLO FROM CLIENT")) {
			return fmt.Errorf("unexpected result from listener: %q", out)
		}

		return nil
	}

	talker := func(ctx context.Context) error {
		// Wait until listener is ready before trying anything
		for {
			_, err := c.SSH(dest, "sudo netstat -tulpn | grep 9988")
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

		srcCmd := fmt.Sprintf(`echo "HELLO FROM CLIENT" | sudo podman run -i ncat ncat %s 9988`, dest.PrivateIP())
		out, err := c.SSH(src, srcCmd)
		if err != nil {
			return err
		}

		if !bytes.Equal(out, []byte("HELLO FROM SERVER")) {
			return fmt.Errorf(`unexpected result from listener: "%v"`, out)
		}

		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	if err := worker.Parallel(ctx, listener, talker); err != nil {
		c.Fatal(err)
	}
}

// Test: Verify basic container network connectivity
func podmanNetworksReliably(c cluster.TestCluster) {
	m := c.Machines()[0]

	tutil.GenPodmanScratchContainer(c, m, "ping", []string{"sh", "ping"})

	output := c.MustSSH(m, `for i in $(seq 1 100); do
		echo -n "$i: "
		sudo podman run --rm ping sh -c 'ping -i 0.2 10.88.0.1 -w 1 >/dev/null && echo PASS || echo FAIL'
	done`)

	numPass := strings.Count(string(output), "PASS")

	if numPass != 100 {
		c.Fatalf("Expected 100 passes, but output was: %s", output)
	}
}
