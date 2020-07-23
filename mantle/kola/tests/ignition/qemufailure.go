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

package ignition

import (
	"fmt"
	"time"

	ignv3types "github.com/coreos/ignition/v2/config/v3_0/types"
	"github.com/pkg/errors"

	"github.com/coreos/mantle/kola"
	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/util"
)

func init() {
	register.RegisterTest(&register.Test{
		Name:        "coreos.ignition.failure",
		Run:         runIgnitionFailure,
		ClusterSize: 0,
		Platforms:   []string{"qemu-unpriv"},
		Tags:        []string{"ignition"},
	})
}

func runIgnitionFailure(c cluster.TestCluster) {
	if err := ignitionFailure(c); err != nil {
		c.Fatal(err.Error())
	}
}

func ignitionFailure(c cluster.TestCluster) error {
	// We can't create files in / due to the immutable bit OSTree creates, so
	// this is a convenient way to test Ignition failure.
	failConfig := ignv3types.Config{
		Ignition: ignv3types.Ignition{
			Version: "3.0.0",
		},
		Storage: ignv3types.Storage{
			Files: []ignv3types.File{
				{
					Node: ignv3types.Node{
						Path: "/notwritable.txt",
					},
					FileEmbedded1: ignv3types.FileEmbedded1{
						Contents: ignv3types.FileContents{
							Source: util.StrToPtr("data:,hello%20world%0A"),
						},
						Mode: util.IntToPtr(420),
					},
				},
			},
		},
	}
	builder := platform.NewBuilder()
	defer builder.Close()
	builder.SetConfig(failConfig, kola.Options.IgnitionVersion == "v2")
	builder.AddPrimaryDisk(&platform.Disk{
		BackingFile: kola.QEMUOptions.DiskImage,
	})
	builder.Memory = 1024
	inst, err := builder.Exec()
	if err != nil {
		return err
	}
	defer inst.Destroy()
	errchan := make(chan error)
	go func() {
		err := inst.WaitAll()
		if err == nil {
			err = fmt.Errorf("Ignition unexpectedly succeeded")
		} else if err == platform.ErrInitramfsEmergency {
			// The expected case
			err = nil
		} else {
			err = errors.Wrapf(err, "expected initramfs emergency.target error")
		}
		errchan <- err
	}()
	select {
	case <-time.After(2 * time.Minute):
		inst.Kill()
		return errors.New("timed out waiting for initramfs error")
	case err := <-errchan:
		if err != nil {
			return err
		}
		return nil
	}
}
