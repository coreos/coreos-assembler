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
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"

	"github.com/coreos/mantle/kola"
	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/conf"
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
	failConfig, err := conf.EmptyIgnition().Render(conf.FailWarnings)
	if err != nil {
		return errors.Wrapf(err, "creating empty config")
	}
	failConfig.AddFile("/notwritable.txt", "Hello world", 0644)

	builder := platform.NewQemuBuilder()
	defer builder.Close()
	builder.SetConfig(failConfig)
	err = builder.AddBootDisk(&platform.Disk{
		BackingFile: kola.QEMUOptions.DiskImage,
	})
	if err != nil {
		return err
	}
	builder.Memory = 1024
	builder.Firmware = kola.QEMUOptions.Firmware
	inst, err := builder.Exec()
	if err != nil {
		return err
	}
	defer inst.Destroy()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	errchan := make(chan error)
	go func() {
		err := inst.WaitAll(ctx)
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
	case <-ctx.Done():
		if err := inst.Kill(); err != nil {
			return errors.Wrapf(err, "failed to kill the vm instance")
		}
		return errors.Wrapf(ctx.Err(), "timed out waiting for initramfs error")
	case err := <-errchan:
		if err != nil {
			return err
		}
		return nil
	}
}
