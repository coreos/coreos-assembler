// Copyright 2016 CoreOS, Inc.
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

package docker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/net/context"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/lang/worker"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/conf"
	"github.com/coreos/mantle/util"
)

type simplifiedDockerInfo struct {
	ServerVersion string
	Driver        string
	CgroupDriver  string
	Runtimes      map[string]struct {
		Path string `json:"path"`
	}
	ContainerdCommit struct {
		ID       string
		Expected string
	}
	RuncCommit struct {
		ID       string
		Expected string
	}
	SecurityOptions []string
}

func init() {
	register.RegisterTest(&register.Test{
		Run:         dockerNetwork,
		ClusterSize: 2,
		Name:        "docker.network",
		Distros:     []string{"cl"},

		// qemu-unpriv machines cannot communicate
		ExcludePlatforms: []string{"qemu-unpriv"},
	})
	register.RegisterTest(&register.Test{
		Run:         dockerOldClient,
		ClusterSize: 0,
		Name:        "docker.oldclient",
		Distros:     []string{"cl"},
	})
	register.RegisterTest(&register.Test{
		Run:         dockerUserns,
		ClusterSize: 1,
		Name:        "docker.userns",
		Distros:     []string{"cl"},
		// TODO: Update for Fedora CoreOS
		UserData: conf.Butane(`
variant: fcos
version: 1.3.0
systemd:
  units:
  - name: docker.service
    enable: true
    dropins:
      - name: 10-userns.conf
        contents: |-
          [Service]
          Environment=DOCKER_OPTS=--userns-remap=dockremap
storage:
  files:
  - filesystem: root
    path: /etc/subuid
    contents:
      inline: "dockremap:100000:65536"
    mode: 0644
  - filesystem: root
    path: /etc/subgid
    contents:
      inline: "dockremap:100000:65536"
    mode: 0644
passwd:
  users:
  - name: dockremap`),

		// qemu-unpriv machines cannot communicate
		ExcludePlatforms: []string{"qemu-unpriv"},
	})

	// This test covers all functionality that should be quick to run and can be
	// run:
	// 1. On an entirely default docker configuration on CL
	// 2. On a 'dirty machine' (in that other tests have already potentially run)
	//
	// Note, being able to run in parallel is desirable for these tests, but not
	// required. Parallelism should be tweaked at the subtest level in the
	// 'dockerBaseTests' implementation
	// The primary goal of using subtests here is to make things quicker to run.
	register.RegisterTest(&register.Test{
		Run:         dockerBaseTests,
		ClusterSize: 1,
		Name:        `docker.base`,
		Distros:     []string{"cl"},
	})

	register.RegisterTest(&register.Test{
		Run:         func(c cluster.TestCluster) { testDockerInfo("btrfs", c) },
		ClusterSize: 1,
		Name:        "docker.btrfs-storage",
		// Note: copied verbatim from https://github.com/coreos/docs/blob/master/os/mounting-storage.md#creating-and-mounting-a-btrfs-volume-file
		// TODO: Update for Fedora CoreOS
		UserData: conf.Butane(`
variant: fcos
version: 1.3.0
systemd:
  units:
    - name: format-var-lib-docker.service
      enable: true
      contents: |
        [Unit]
        Before=docker.service var-lib-docker.mount
        ConditionPathExists=!/var/lib/docker.btrfs
        [Service]
        Type=oneshot
        ExecStart=/usr/bin/truncate --size=25G /var/lib/docker.btrfs
        ExecStart=/usr/sbin/mkfs.btrfs /var/lib/docker.btrfs
        [Install]
        WantedBy=multi-user.target
    - name: var-lib-docker.mount
      enable: true
      contents: |
        [Unit]
        Before=docker.service
        After=format-var-lib-docker.service
        Requires=format-var-lib-docker.service
        [Install]
        RequiredBy=docker.service
        [Mount]
        What=/var/lib/docker.btrfs
        Where=/var/lib/docker
        Type=btrfs
        Options=loop,discard`),
		Distros: []string{"cl"},
	})

	register.RegisterTest(&register.Test{
		// For a while we shipped /usr/lib/coreos/dockerd as the execstart of the
		// docker systemd unit.
		// This test verifies backwards compatibility with that unit to ensure
		// users who copied it into /etc aren't broken.
		Name:        "docker.lib-coreos-dockerd-compat",
		Run:         dockerBaseTests,
		ClusterSize: 1,
		Distros:     []string{"cl"},
		// TODO: Update for Fedora CoreOS
		UserData: conf.Butane(`
variant: fcos
version: 1.3.0
systemd:
  units:
  - name: docker.service
    contents: |-
      [Unit]
      Description=Docker Application Container Engine
      Documentation=http://docs.docker.com
      After=containerd.service docker.socket network.target
      Requires=containerd.service docker.socket

      [Service]
      Type=notify
      EnvironmentFile=-/run/flannel/flannel_docker_opts.env

      # the default is not to use systemd for cgroups because the delegate issues still
      # exists and systemd currently does not support the cgroup feature set required
      # for containers run by docker
      ExecStart=/usr/lib/coreos/dockerd --host=fd:// --containerd=/var/run/docker/libcontainerd/docker-containerd.sock $DOCKER_OPTS $DOCKER_CGROUPS $DOCKER_OPT_BIP $DOCKER_OPT_MTU $DOCKER_OPT_IPMASQ
      ExecReload=/bin/kill -s HUP $MAINPID
      LimitNOFILE=1048576
      # Having non-zero Limit*s causes performance problems due to accounting overhead
      # in the kernel. We recommend using cgroups to do container-local accounting.
      LimitNPROC=infinity
      LimitCORE=infinity
      # Uncomment TasksMax if your systemd version supports it.
      # Only systemd 226 and above support this version.
      TasksMax=infinity
      TimeoutStartSec=0
      # set delegate yes so that systemd does not reset the cgroups of docker containers
      Delegate=yes

      [Install]
      WantedBy=multi-user.target`),
	})
	register.RegisterTest(&register.Test{
		// Ensure containerd gets back up when it dies
		Name:        "docker.containerd-restart",
		Run:         dockerContainerdRestart,
		ClusterSize: 1,
		Distros:     []string{"cl"},
		// TODO: Update for Fedora CoreOS
		UserData: conf.Butane(`
variant: fcos
version: 1.3.0
systemd:
  units:
   - name: docker.service
     enable: true`),

		// https://github.com/coreos/mantle/issues/999
		// On the qemu-unpriv platform the DHCP provides no data, pre-systemd 241 the DHCP server sending
		// no routes to the link to spin in the configuring state. docker.service pulls in the network-online
		// target which causes the basic machine checks to fail
		ExcludePlatforms: []string{"qemu-unpriv"},
	})
}

// make a docker container out of binaries on the host
func genDockerContainer(c cluster.TestCluster, m platform.Machine, name string, binnames []string) {
	cmd := `tmpdir=$(mktemp -d); cd $tmpdir; echo -e "FROM scratch\nCOPY . /" > Dockerfile;
	        b=$(which %s); libs=$(sudo ldd $b | grep -o /lib'[^ ]*' | sort -u);
	        sudo rsync -av --relative --copy-links $b $libs ./;
	        sudo docker build -t %s .`

	c.RunCmdSyncf(m, cmd, strings.Join(binnames, " "), name)
}

func dockerBaseTests(c cluster.TestCluster) {
	c.Run("docker-info", func(c cluster.TestCluster) {
		testDockerInfo("overlay", c)
	})
	c.Run("resources", dockerResources)
	c.Run("networks-reliably", dockerNetworksReliably)
	c.Run("user-no-caps", dockerUserNoCaps)
}

// using a simple container, exercise various docker options that set resource
// limits. also acts as a regression test for
// https://github.com/coreos/bugs/issues/1246.
func dockerResources(c cluster.TestCluster) {
	m := c.Machines()[0]

	genDockerContainer(c, m, "sleep", []string{"sleep"})

	dockerFmt := "docker run --rm %s sleep sleep 0.2"

	dCmd := func(arg string) string {
		return fmt.Sprintf(dockerFmt, arg)
	}

	ctx := context.Background()
	wg := worker.NewWorkerGroup(ctx, 10)

	// ref https://docs.docker.com/engine/reference/run/#runtime-constraints-on-resources
	for _, dockerCmd := range []string{
		// must set memory when setting memory-swap
		dCmd("--memory=20m --memory-swap=20m"),
		dCmd("--memory-reservation=10m"),
		dCmd("--kernel-memory=10m"),
		dCmd("--cpu-shares=100"),
		dCmd("--cpu-period=1000"),
		dCmd("--cpuset-cpus=0"),
		dCmd("--cpuset-mems=0"),
		dCmd("--cpu-quota=1000"),
		dCmd("--blkio-weight=10"),
		// none of these work in QEMU due to apparent lack of cfq for
		// blkio in virtual block devices.
		//dCmd("--blkio-weight-device=/dev/vda:10"),
		//dCmd("--device-read-bps=/dev/vda:1kb"),
		//dCmd("--device-write-bps=/dev/vda:1kb"),
		//dCmd("--device-read-iops=/dev/vda:10"),
		//dCmd("--device-write-iops=/dev/vda:10"),
		dCmd("--memory=20m --oom-kill-disable=true"),
		dCmd("--memory-swappiness=50"),
		dCmd("--shm-size=1m"),
	} {
		// lol closures
		cmd := dockerCmd

		worker := func(ctx context.Context) error {
			// TODO: pass context thru to SSH
			output, err := c.SSH(m, cmd)
			if err != nil {
				return fmt.Errorf("failed to run %q: output: %q status: %q", cmd, output, err)
			}
			return nil
		}

		if err := wg.Start(worker); err != nil {
			c.Fatal(wg.WaitError(err))
		}
	}

	if err := wg.Wait(); err != nil {
		c.Fatal(err)
	}
}

// Ensure that docker containers can make network connections outside of the host
func dockerNetwork(c cluster.TestCluster) {
	machines := c.Machines()
	src, dest := machines[0], machines[1]

	c.Log("creating ncat containers")

	genDockerContainer(c, src, "ncat", []string{"ncat"})
	genDockerContainer(c, dest, "ncat", []string{"ncat"})

	listener := func(ctx context.Context) error {
		// Will block until a message is recieved
		out, err := c.SSH(dest,
			`echo "HELLO FROM SERVER" | docker run -i -p 9988:9988 ncat ncat --idle-timeout 20 --listen 0.0.0.0 9988`,
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
			_, err := c.SSH(dest, "sudo lsof -i TCP:9988 -s TCP:LISTEN | grep 9988 -q")
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

		srcCmd := fmt.Sprintf(`echo "HELLO FROM CLIENT" | docker run -i ncat ncat %s 9988`, dest.PrivateIP())
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

// Regression test for https://github.com/coreos/bugs/issues/1569 and
// https://github.com/coreos/docker/pull/31
func dockerOldClient(c cluster.TestCluster) {
	oldclient := "/usr/lib/kola/amd64/docker-1.9.1"
	if _, err := os.Stat(oldclient); err != nil {
		c.Skipf("Can't find old docker client to test: %v", err)
	}

	m, err := c.NewMachine(nil)
	if err != nil {
		c.Fatal(err)
	}
	if err := cluster.DropFile(c.Machines(), oldclient); err != nil {
		c.Error(err)
	}

	genDockerContainer(c, m, "echo", []string{"echo"})

	output := c.MustSSH(m, "/home/core/docker-1.9.1 run echo echo 'IT WORKED'")

	if !bytes.Equal(output, []byte("IT WORKED")) {
		c.Fatalf("unexpected result from docker client: %q", output)
	}
}

// Regression test for userns breakage under 1.12
func dockerUserns(c cluster.TestCluster) {
	m := c.Machines()[0]

	genDockerContainer(c, m, "userns-test", []string{"echo", "sleep"})

	c.RunCmdSync(m, `sudo setenforce 1`)
	output := c.MustSSH(m, `docker run userns-test echo fj.fj`)
	if !bytes.Equal(output, []byte("fj.fj")) {
		c.Fatalf("expected fj.fj, got %s", string(output))
	}

	// And just in case, verify that a container really is userns remapped
	c.RunCmdSync(m, `docker run -d --name=sleepy userns-test sleep 10000`)
	uid_map := c.MustSSH(m, `until [[ "$(docker inspect -f {{.State.Running}} sleepy)" == "true" ]]; do sleep 0.1; done;
		pid=$(docker inspect -f {{.State.Pid}} sleepy);
		cat /proc/$pid/uid_map; docker kill sleepy &>/dev/null`)
	// uid_map is of the form `$mappedNamespacePidStart   $realNamespacePidStart
	// $rangeLength`. We expect `0     100000      65536`
	mapParts := strings.Fields(strings.TrimSpace(string(uid_map)))
	if len(mapParts) != 3 {
		c.Fatalf("expected uid_map to have three parts, was: %s", string(uid_map))
	}
	if mapParts[0] != "0" && mapParts[1] != "100000" {
		c.Fatalf("unexpected userns mapping values: %v", string(uid_map))
	}
}

// Regression test for https://github.com/coreos/bugs/issues/1785
// Also, hopefully will catch any similar issues
func dockerNetworksReliably(c cluster.TestCluster) {
	m := c.Machines()[0]

	genDockerContainer(c, m, "ping", []string{"sh", "ping"})

	output := c.MustSSH(m, `for i in $(seq 1 100); do
		echo -n "$i: "
		docker run --rm ping sh -c 'ping -i 0.2 172.17.0.1 -w 1 >/dev/null && echo PASS || echo FAIL'
	done`)

	numPass := strings.Count(string(output), "PASS")

	if numPass != 100 {
		c.Fatalf("Expected 100 passes, but output was: %s", output)
	}

}

// Regression test for CVE-2016-8867
// CVE-2016-8867 gave a container capabilities, including fowner, even if it
// was a non-root user.
// We test that a user inside a container does not have any effective nor
// permitted capabilities (which is what the cve was).
// For good measure, we also check that fs permissions deny that user from
// accessing /root.
func dockerUserNoCaps(c cluster.TestCluster) {
	m := c.Machines()[0]

	genDockerContainer(c, m, "captest", []string{"capsh", "sh", "grep", "cat", "ls"})

	output := c.MustSSH(m, `docker run --user 1000:1000 \
		-v /root:/root \
		captest sh -c \
		'cat /proc/self/status | grep -E "Cap(Eff|Prm)"; ls /root &>/dev/null && echo "FAIL: could read root" || echo "PASS: err reading root"'`)

	outputlines := strings.Split(string(output), "\n")
	if len(outputlines) < 3 {
		c.Fatalf("expected two lines of caps and an an error/succcess line. Got %q", string(output))
	}
	cap1, cap2 := strings.Fields(outputlines[0]), strings.Fields(outputlines[1])
	// The format of capabilities in /proc/*/status is e.g.: CapPrm:\t0000000000000000
	// We could parse the hex to its actual capabilities, but since we're looking for none, just checking it's all 0 is good enough.
	if len(cap1) != 2 || len(cap2) != 2 {
		c.Fatalf("capability lines didn't have two parts: %q", string(output))
	}
	if cap1[1] != "0000000000000000" || cap2[1] != "0000000000000000" {
		c.Fatalf("Permitted / effective capabilities were non-zero: %q", string(output))
	}

	// Finally, check for fail/success on reading /root
	if !strings.HasPrefix(outputlines[len(outputlines)-1], "PASS: ") {
		c.Fatalf("reading /root test failed: %q", string(output))
	}
}

// dockerContainerdRestart ensures containerd will restart if it dies. It tests that containerd is running,
// kills it, the tests that it came back up.
func dockerContainerdRestart(c cluster.TestCluster) {
	m := c.Machines()[0]

	pid := c.MustSSH(m, "systemctl show containerd -p MainPID --value")
	if string(pid) == "0" {
		c.Fatalf("Could not find containerd pid")
	}

	testContainerdUp(c)

	// kill it
	c.RunCmdSync(m, "sudo kill "+string(pid))

	// retry polling its state
	if err := util.Retry(12, 6*time.Second, func() error {
		state := c.MustSSH(m, "systemctl show containerd -p SubState --value")
		switch string(state) {
		case "running":
			return nil
		case "stopped", "exited", "failed":
			c.Fatalf("containerd entered stopped state")
		}
		return fmt.Errorf("containerd failed to restart")
	}); err != nil {
		c.Error(err)
	}

	// verify systemd started it and that it's pid is different
	newPid := c.MustSSH(m, "systemctl show containerd -p MainPID --value")
	if string(newPid) == "0" {
		c.Fatalf("Containerd is not running (could not find pid)")
	} else if string(newPid) == string(pid) {
		c.Fatalf("Old and new pid's are the same. containerd did not die")
	}

	// verify it came back and docker knows about it
	testContainerdUp(c)
}

func testContainerdUp(c cluster.TestCluster) {
	m := c.Machines()[0]

	info, err := getDockerInfo(c, m)
	if err != nil {
		c.Fatal(err)
	}

	if info.ContainerdCommit.ID != info.ContainerdCommit.Expected {
		c.Fatalf("Docker could not find containerd")
	}
}

func getDockerInfo(c cluster.TestCluster, m platform.Machine) (simplifiedDockerInfo, error) {
	dockerInfoJson, err := c.SSH(m, `curl -s --unix-socket /var/run/docker.sock http://docker/v1.24/info`)
	if err != nil {
		return simplifiedDockerInfo{}, fmt.Errorf("could not get dockerinfo: %v", err)
	}

	target := simplifiedDockerInfo{}

	err = json.Unmarshal(dockerInfoJson, &target)
	if err != nil {
		return simplifiedDockerInfo{}, fmt.Errorf("could not unmarshal dockerInfo %q into known json: %v", string(dockerInfoJson), err)
	}

	return target, nil
}

// testDockerInfo test that docker info's output is as expected.  the expected
// filesystem may be asserted as one of 'overlay', 'btrfs', 'devicemapper'
// depending on how the machine was launched.
func testDockerInfo(expectedFs string, c cluster.TestCluster) {
	m := c.Machines()[0]

	info, err := getDockerInfo(c, m)
	if err != nil {
		c.Fatal(err)
	}

	// Canonicalize info
	sort.Strings(info.SecurityOptions)

	// Because we prefer overlay2/overlay for different docker versions, figure
	// out the correct driver to be testing for based on our docker version.
	expectedOverlayDriver := "overlay2"
	if strings.HasPrefix(info.ServerVersion, "1.12.") || strings.HasPrefix(info.ServerVersion, "17.04.") {
		expectedOverlayDriver = "overlay"
	}

	expectedFsDriverMap := map[string]string{
		"overlay":      expectedOverlayDriver,
		"btrfs":        "btrfs",
		"devicemapper": "devicemapper",
	}

	expectedFsDriver := expectedFsDriverMap[expectedFs]
	if info.Driver != expectedFsDriver {
		c.Errorf("unexpected driver: %v != %v", expectedFsDriver, info.Driver)
	}

	// Validations shared by all versions currently
	if !reflect.DeepEqual(info.SecurityOptions, []string{"seccomp", "selinux"}) {
		c.Errorf("unexpected security options: %+v", info.SecurityOptions)
	}

	if info.CgroupDriver != "cgroupfs" {
		c.Errorf("unexpected cgroup driver %v", info.CgroupDriver)
	}

	if info.ContainerdCommit.ID != info.ContainerdCommit.Expected {
		c.Errorf("commit mismatch for containerd: %v != %v", info.ContainerdCommit.Expected, info.ContainerdCommit.ID)
	}

	if info.RuncCommit.ID != info.RuncCommit.Expected {
		c.Errorf("commit mismatch for runc: %v != %v", info.RuncCommit.Expected, info.RuncCommit.ID)
	}

	if runcInfo, ok := info.Runtimes["runc"]; ok {
		if runcInfo.Path == "" {
			c.Errorf("expected non-empty runc path")
		}
	} else {
		c.Errorf("runc was not in runtimes: %+v", info.Runtimes)
	}
}
