// Copyright 2019 Red Hat
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

package unprivqemu

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/pborman/uuid"

	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/conf"
	"github.com/coreos/mantle/system/exec"
	"github.com/coreos/mantle/util"
)

// Cluster is a local cluster of QEMU-based virtual machines.
//
// XXX: must be exported so that certain QEMU tests can access struct members
// through type assertions.
type Cluster struct {
	*platform.BaseCluster
	flight *flight

	mu sync.Mutex
}

func (qc *Cluster) NewMachine(userdata *conf.UserData) (platform.Machine, error) {
	return qc.NewMachineWithOptions(userdata, platform.MachineOptions{}, true)
}

func (qc *Cluster) NewMachineWithOptions(userdata *conf.UserData, options platform.MachineOptions, pdeathsig bool) (platform.Machine, error) {
	id := uuid.New()

	dir := filepath.Join(qc.RuntimeConf().OutputDir, id)
	if err := os.Mkdir(dir, 0777); err != nil {
		return nil, err
	}

	// hacky solution for cloud config ip substitution
	// NOTE: escaping is not supported
	qc.mu.Lock()

	conf, err := qc.RenderUserData(userdata, map[string]string{})
	if err != nil {
		qc.mu.Unlock()
		return nil, err
	}
	qc.mu.Unlock()

	var confPath string
	if conf.IsIgnition() {
		confPath = filepath.Join(dir, "ignition.json")
		if err := conf.WriteFile(confPath); err != nil {
			return nil, err
		}
	} else if conf.IsEmpty() {
	} else {
		return nil, fmt.Errorf("unprivileged qemu only supports Ignition or empty configs")
	}

	journal, err := platform.NewJournal(dir)
	if err != nil {
		return nil, err
	}

	qm := &machine{
		qc:          qc,
		id:          id,
		journal:     journal,
		consolePath: filepath.Join(dir, "console.txt"),
	}

	qmCmd, extraFiles, err := platform.CreateQEMUCommand(qc.flight.opts.Board, qm.id, qc.flight.opts.BIOSImage, qm.consolePath, confPath, qc.flight.diskImagePath, conf.IsIgnition(), options)
	if err != nil {
		return nil, err
	}

	for _, file := range extraFiles {
		defer file.Close()
	}

	qc.mu.Lock()

	qmCmd = append(qmCmd, "-netdev", "user,id=eth0,restrict=yes,hostfwd=tcp:127.0.0.1:0-:22", "-device", platform.Virtio(qc.flight.opts.Board, "net", "netdev=eth0"))

	plog.Debugf("NewMachine: %q", qmCmd)

	qm.qemu = exec.Command(qmCmd[0], qmCmd[1:]...)

	qc.mu.Unlock()

	cmd := qm.qemu.(*exec.ExecCmd)
	cmd.Stderr = os.Stderr

	if pdeathsig {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Pdeathsig: syscall.SIGTERM,
		}
	}

	cmd.ExtraFiles = append(cmd.ExtraFiles, extraFiles...)

	if err = qm.qemu.Start(); err != nil {
		return nil, err
	}

	pid := strconv.Itoa(qm.qemu.Pid())
	err = util.Retry(6, 5*time.Second, func() error {
		var err error
		qm.ip, err = getAddress(pid)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if err := platform.StartMachine(qm, qm.journal); err != nil {
		qm.Destroy()
		return nil, err
	}

	qc.AddMach(qm)

	return qm, nil
}

func (qc *Cluster) Destroy() {
	qc.BaseCluster.Destroy()
	qc.flight.DelCluster(qc)
}

// parse /proc/net/tcp to determine the port selected by QEMU
func getAddress(pid string) (string, error) {
	data, err := ioutil.ReadFile("/proc/net/tcp")
	if err != nil {
		return "", fmt.Errorf("reading /proc/net/tcp: %v", err)
	}

	for _, line := range strings.Split(string(data), "\n")[1:] {
		fields := strings.Fields(line)
		if len(fields) < 10 {
			// at least 10 fields are neeeded for the local & remote address and the inode
			continue
		}
		localAddress := fields[1]
		remoteAddress := fields[2]
		inode := fields[9]

		isLocalPat := regexp.MustCompile("0100007F:[[:xdigit:]]{4}")
		if !isLocalPat.MatchString(localAddress) || remoteAddress != "00000000:0000" {
			continue
		}

		dir := fmt.Sprintf("/proc/%s/fd/", pid)
		fds, err := ioutil.ReadDir(dir)
		if err != nil {
			return "", fmt.Errorf("listing %s: %v", dir, err)
		}

		for _, f := range fds {
			link, err := os.Readlink(filepath.Join(dir, f.Name()))
			if err != nil {
				continue
			}
			socketPattern := regexp.MustCompile("socket:\\[([0-9]+)\\]")
			match := socketPattern.FindStringSubmatch(link)
			if len(match) > 1 {
				if inode == match[1] {
					// this entry belongs to the QEMU pid, parse the port and return the address
					portHex := strings.Split(localAddress, ":")[1]
					port, err := strconv.ParseInt(portHex, 16, 32)
					if err != nil {
						return "", fmt.Errorf("decoding port %q: %v", portHex, err)
					}
					return fmt.Sprintf("127.0.0.1:%d", port), nil
				}
			}
		}
	}
	return "", fmt.Errorf("didn't find an address")
}
