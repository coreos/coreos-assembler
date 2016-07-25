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
	"fmt"
	"io"

	"github.com/coreos/go-semver/semver"
	"github.com/coreos/pkg/capnslog"
	"golang.org/x/net/context"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/lang/worker"
	"github.com/coreos/mantle/system/exec"
	"github.com/coreos/mantle/system/targen"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "kola/tests/docker")
)

func init() {
	register.Register(&register.Test{
		Run:         dockerResources,
		ClusterSize: 1,
		NativeFuncs: map[string]func() error{
			"sleepcontainer": func() error {
				return genDockerContainer("sleep", []string{"sleep"})
			},
		},
		Name:     "docker.resources",
		UserData: `#cloud-config`,
		// began shipping docker 1.10 in 949, which has all of the
		// tested resource options.
		MinVersion: semver.Version{Major: 949},
	})
}

// executed on the target vm to make a docker container out of binaries on the host
func genDockerContainer(name string, binnames []string) error {
	tg := targen.New()

	for _, bin := range binnames {
		binpath, err := exec.LookPath(bin)
		if err != nil {
			return fmt.Errorf("failed to find %q binary: %v", bin, err)
		}

		tg.AddBinary(binpath)
	}

	pr, pw := io.Pipe()
	dimport := exec.Command("docker", "import", "-", name)
	dimport.Stdin = pr

	if err := dimport.Start(); err != nil {
		return fmt.Errorf("starting docker import failed %v", err)
	}

	if err := tg.Generate(pw); err != nil {
		return fmt.Errorf("failed to generate tarball: %v", err)
	}

	// err is always nil.
	_ = pw.Close()

	if err := dimport.Wait(); err != nil {
		return fmt.Errorf("waiting for docker import failed %v", err)
	}

	return nil
}

// using a simple container, exercise various docker options that set resource
// limits. also acts as a regression test for
// https://github.com/coreos/bugs/issues/1246.
func dockerResources(c cluster.TestCluster) error {
	m := c.Machines()[0]

	plog.Debug("creating sleep container")

	if err := c.RunNative("sleepcontainer", m); err != nil {
		return fmt.Errorf("failed to create sleep container: %v", err)
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
