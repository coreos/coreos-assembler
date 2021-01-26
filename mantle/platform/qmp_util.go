// Copyright 2020 Red Hat, Inc.
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

// The Qemu Machine Protocol - to remotely query and operate a qemu instance (https://wiki.qemu.org/Documentation/QMP)

package platform

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/coreos/mantle/util"
	"github.com/pkg/errors"

	"github.com/digitalocean/go-qemu/qmp"
)

// QOMDev is a QMP monitor, for interactions with a QEMU instance.
type QOMDev struct {
	Return []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"return"`
}

type QOMBlkDev struct {
	Return []struct {
		Device     string `json:"device"`
		DevicePath string `json:"qdev"`
		Removable  bool   `json:"removable"`
		Inserted   struct {
			BackingFileDepth int `json:"backing_file_depth"`
		} `json:"inserted"`
	} `json:"return"`
}

// Create a new QMP socket connection
func newQMPMonitor(sockaddr string) (*qmp.SocketMonitor, error) {
	qmpPath := filepath.Join(sockaddr, "qmp.sock")
	var monitor *qmp.SocketMonitor
	var err error
	if err := util.Retry(10, 1*time.Second, func() error {
		monitor, err = qmp.NewSocketMonitor("unix", qmpPath, 2*time.Second)
		if err != nil {
			return err
		}
		return nil
	}); err != nil {
		return nil, errors.Wrapf(err, "Connecting to qemu monitor")
	}
	return monitor, nil
}

// Executes a query which provides the list of devices and their names
func listQMPDevices(monitor *qmp.SocketMonitor, sockaddr string) (*QOMDev, error) {
	listcmd := []byte(`{ "execute": "qom-list", "arguments": { "path": "/machine/peripheral-anon" } }`)
	out, err := monitor.Run(listcmd)
	if err != nil {
		return nil, errors.Wrapf(err, "Running QMP list command")
	}

	var devs QOMDev
	if err = json.Unmarshal(out, &devs); err != nil {
		return nil, errors.Wrapf(err, "De-serializing QMP output")
	}
	return &devs, nil
}

// Executes a query which provides the list of block devices and their names
func listQMPBlkDevices(monitor *qmp.SocketMonitor, sockaddr string) (*QOMBlkDev, error) {
	listcmd := []byte(`{ "execute": "query-block" }`)
	out, err := monitor.Run(listcmd)
	if err != nil {
		return nil, errors.Wrapf(err, "Running QMP list command")
	}

	var devs QOMBlkDev
	if err = json.Unmarshal(out, &devs); err != nil {
		return nil, errors.Wrapf(err, "De-serializing QMP output")
	}
	return &devs, nil
}

// Set the bootindex for the particular device
func setBootIndexForDevice(monitor *qmp.SocketMonitor, device string, bootindex int) error {
	cmd := fmt.Sprintf(`{ "execute":"qom-set", "arguments": { "path":"%s", "property":"bootindex", "value":%d } }`, device, bootindex)
	if _, err := monitor.Run([]byte(cmd)); err != nil {
		return errors.Wrapf(err, "Running QMP command")
	}
	return nil
}

// Delete a block device for the particular qemu instance
func deleteBlockDevice(monitor *qmp.SocketMonitor, device string) error {
	cmd := fmt.Sprintf(`{ "execute": "device_del", "arguments": { "id":"%s" } }`, device)
	if _, err := monitor.Run([]byte(cmd)); err != nil {
		return errors.Wrapf(err, "Running QMP command")
	}
	return nil
}
