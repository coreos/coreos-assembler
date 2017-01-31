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
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/coreos/go-semver/semver"
	"github.com/coreos/pkg/capnslog"
	"golang.org/x/crypto/ssh"
	"golang.org/x/net/context"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/kola/skip"
	"github.com/coreos/mantle/lang/worker"
	"github.com/coreos/mantle/platform"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "kola/tests/docker")
)

func init() {
	register.Register(&register.Test{
		Run:         dockerResources,
		ClusterSize: 1,
		Name:        "docker.resources",
		UserData:    `#cloud-config`,
		// began shipping docker 1.10 in 949, which has all of the
		// tested resource options.
		MinVersion: semver.Version{Major: 949},
	})
	register.Register(&register.Test{
		Run:         dockerNetwork,
		ClusterSize: 2,
		Name:        "docker.network",
		UserData:    `#cloud-config`,

		MinVersion: semver.Version{Major: 1192},
	})
	register.Register(&register.Test{
		Run:         dockerOldClient,
		ClusterSize: 1,
		Name:        "docker.oldclient",
		UserData:    `#cloud-config`,
		MinVersion:  semver.Version{Major: 1192},
	})
	register.Register(&register.Test{
		Run:         dockerUserns,
		ClusterSize: 1,
		Name:        "docker.userns",
		// Source yaml:
		// https://github.com/coreos/container-linux-config-transpiler
		/*
			systemd:
			  units:
			  - name: docker.service
			    enable: true
			    dropins:
			      - name: 10-uesrns.conf
			        contents: |-
			          [Service]
			          Environment=DOCKER_OPTS=--userns-remap=dockremap
			storage:
			  files:
			  - filesystem: root
			    path: /etc/subuid
			    contents:
			      inline: "dockremap:100000:65536"
			  - filesystem: root
			    path: /etc/subgid
			    contents:
			      inline: "dockremap:100000:65536"
			passwd:
			  users:
			  - name: dockremap
			    create: {}
		*/
		Platforms:  []string{"aws", "gce"},
		UserData:   `{"ignition":{"version":"2.0.0","config":{}},"storage":{"files":[{"filesystem":"root","path":"/etc/subuid","contents":{"source":"data:,dockremap%3A100000%3A65536","verification":{}},"user":{},"group":{}},{"filesystem":"root","path":"/etc/subgid","contents":{"source":"data:,dockremap%3A100000%3A65536","verification":{}},"user":{},"group":{}}]},"systemd":{"units":[{"name":"docker.service","enable":true,"dropins":[{"name":"10-uesrns.conf","contents":"[Service]\nEnvironment=DOCKER_OPTS=--userns-remap=dockremap"}]}]},"networkd":{},"passwd":{"users":[{"name":"dockremap","create":{}}]}}`,
		MinVersion: semver.Version{Major: 1192},
	})
}

// make a docker container out of binaries on the host
func genDockerContainer(m platform.Machine, name string, binnames []string) error {
	cmd := `tmpdir=$(mktemp -d); cd $tmpdir; echo -e "FROM scratch\nCOPY . /" > Dockerfile;
	        b=$(which %s); libs=$(ldd $b | grep -o /lib'[^ ]*' | sort -u);
	        rsync -av --relative --copy-links $b $libs ./;
	        docker build -t %s .`

	if output, err := m.SSH(fmt.Sprintf(cmd, strings.Join(binnames, " "), name)); err != nil {
		return fmt.Errorf("failed to make %s container: output: %q status: %q", name, output, err)
	}

	return nil
}

// using a simple container, exercise various docker options that set resource
// limits. also acts as a regression test for
// https://github.com/coreos/bugs/issues/1246.
func dockerResources(c cluster.TestCluster) error {
	m := c.Machines()[0]

	plog.Debug("creating sleep container")

	if err := genDockerContainer(m, "sleep", []string{"sleep"}); err != nil {
		return err
	}

	dockerFmt := "docker run --rm %s sleep sleep 0.2"

	dCmd := func(arg string) string {
		return fmt.Sprintf(dockerFmt, arg)
	}

	ctx := context.Background()
	wg := worker.NewWorkerGroup(ctx, 10)

	// ref https://docs.docker.com/engine/reference/run/#runtime-constraints-on-resources
	for _, dockerCmd := range []string{
		// must set memory when setting memory-swap
		dCmd("--memory=10m --memory-swap=10m"),
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
		dCmd("--memory=10m --oom-kill-disable=true"),
		dCmd("--memory-swappiness=50"),
		dCmd("--shm-size=1m"),
	} {
		plog.Debugf("Executing %q", dockerCmd)

		// lol closures
		cmd := dockerCmd

		worker := func(c context.Context) error {
			// TODO: pass context thru to SSH
			output, err := m.SSH(cmd)
			if err != nil {
				return fmt.Errorf("failed to run %q: output: %q status: %q", dockerCmd, output, err)
			}
			return nil
		}

		if err := wg.Start(worker); err != nil {
			return wg.WaitError(err)
		}
	}

	return wg.Wait()
}

// Ensure that docker containers can make network connections outside of the host
func dockerNetwork(c cluster.TestCluster) error {
	machines := c.Machines()
	src, dest := machines[0], machines[1]

	plog.Debug("creating ncat containers")

	if err := genDockerContainer(src, "ncat", []string{"ncat"}); err != nil {
		return err
	}

	if err := genDockerContainer(dest, "ncat", []string{"ncat"}); err != nil {
		return err
	}

	listener := func(c context.Context) error {
		// Will block until a message is recieved
		out, err := dest.SSH(
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

	talker := func(c context.Context) error {
		// Wait until listener is ready before trying anything
		for {
			_, err := dest.SSH("sudo lsof -i TCP:9988 -s TCP:LISTEN | grep 9988 -q")
			if err == nil {
				break // socket is ready
			}

			exit, ok := err.(*ssh.ExitError)
			if !ok || exit.Waitmsg.ExitStatus() != 1 { // 1 is the expected exit of grep -q
				return err
			}

			plog.Debug("waiting for server to be ready")
			select {
			case <-c.Done():
				return fmt.Errorf("timeout waiting for server")
			default:
				time.Sleep(100 * time.Millisecond)
			}
		}

		srcCmd := fmt.Sprintf(`echo "HELLO FROM CLIENT" | docker run -i ncat ncat %s 9988`, dest.PrivateIP())
		out, err := src.SSH(srcCmd)
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

	return worker.Parallel(ctx, listener, talker)
}

// Regression test for https://github.com/coreos/bugs/issues/1569 and
// https://github.com/coreos/docker/pull/31
func dockerOldClient(c cluster.TestCluster) error {
	oldclient := "/usr/lib/kola/amd64/docker-1.9.1"
	if _, err := os.Stat(oldclient); err != nil {
		return skip.Skip(fmt.Sprintf("Can't find old docker client to test: %v", err))
	}
	c.DropFile(oldclient)

	m := c.Machines()[0]

	if err := genDockerContainer(m, "echo", []string{"echo"}); err != nil {
		return err
	}

	output, err := m.SSH("/home/core/docker-1.9.1 run echo echo 'IT WORKED'")
	if err != nil {
		return fmt.Errorf("failed to run old docker client: %q status: %q", output, err)
	}

	if !bytes.Equal(output, []byte("IT WORKED")) {
		return fmt.Errorf("unexpected result from docker client: %q", output)
	}

	return nil
}

// Regression test for userns breakage under 1.12
func dockerUserns(c cluster.TestCluster) error {
	m := c.Machines()[0]

	if err := genDockerContainer(m, "userns-test", []string{"echo", "sleep"}); err != nil {
		return err
	}

	_, err := m.SSH(`sudo setenforce 1`)
	if err != nil {
		return fmt.Errorf("could not enable selinux")
	}
	output, err := m.SSH(`docker run userns-test echo fj.fj`)
	if err != nil {
		return fmt.Errorf("failed to run echo under userns: output: %q status: %q", output, err)
	}
	if !bytes.Equal(output, []byte("fj.fj")) {
		return fmt.Errorf("expected fj.fj, got %s", string(output))
	}

	// And just in case, verify that a container really is userns remapped
	_, err = m.SSH(`docker run -d --name=sleepy userns-test sleep 10000`)
	if err != nil {
		return fmt.Errorf("could not run sleep: %v", err)
	}
	uid_map, err := m.SSH(`until [[ "$(/usr/bin/docker inspect -f {{.State.Running}} sleepy)" == "true" ]]; do sleep 0.1; done;
	                pid=$(docker inspect -f {{.State.Pid}} sleepy); 
									cat /proc/$pid/uid_map; docker kill sleepy &>/dev/null`)
	if err != nil {
		return fmt.Errorf("could not read uid mapping: %v", err)
	}
	// uid_map is of the form `$mappedNamespacePidStart   $realNamespacePidStart
	// $rangeLength`. We expect `0     100000      65536`
	mapParts := strings.Fields(strings.TrimSpace(string(uid_map)))
	if len(mapParts) != 3 {
		return fmt.Errorf("expected uid_map to have three parts, was: %s", string(uid_map))
	}
	if mapParts[0] != "0" && mapParts[1] != "100000" {
		return fmt.Errorf("unexpected userns mapping values: %v", string(uid_map))
	}

	return nil
}
