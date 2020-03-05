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

package kola

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	ignconverter "github.com/coreos/ign-converter"
	ignv3types "github.com/coreos/ignition/v2/config/v3_0/types"
	"github.com/kballard/go-shellquote"
	"github.com/pkg/errors"

	"github.com/coreos/mantle/platform"
	"github.com/coreos/mantle/platform/conf"
	"github.com/coreos/mantle/util"
)

// kolaExtBinDataDir is where data will be stored
const kolaExtBinDataDir = "/var/opt/kola/extdata"

// kolaExtBinDataEnv is an environment variable pointing to the above
const kolaExtBinDataEnv = "KOLA_EXT_DATA"

func RunExtBin(pltfrm, outputDir, extbin string, extdata string) error {
	if CosaBuild == nil {
		return fmt.Errorf("Must specify --cosa-build")
	}

	plog.Debugf("Creating flight")
	flight, err := NewFlight(pltfrm)
	if err != nil {
		return errors.Wrapf(err, "Creating flight")
	}
	defer flight.Destroy()

	rconf := &platform.RuntimeConfig{
		OutputDir: outputDir,
	}
	plog.Debugf("Creating cluster")
	c, err := flight.NewCluster(rconf)
	if err != nil {
		return err
	}

	unitname := "kola-runext.service"
	remotepath := fmt.Sprintf("/usr/local/bin/kola-runext-%s", filepath.Base(extbin))
	unit := fmt.Sprintf(`[Unit]
[Service]
Type=oneshot
RemainAfterExit=yes
Environment=%s=%s
ExecStart=%s
[Install]
RequiredBy=multi-user.target
`, kolaExtBinDataEnv, kolaExtBinDataDir, remotepath)
	config := ignv3types.Config{
		Ignition: ignv3types.Ignition{
			Version: "3.0.0",
		},
		Systemd: ignv3types.Systemd{
			Units: []ignv3types.Unit{
				{
					Name:     unitname,
					Contents: &unit,
					Enabled:  util.BoolToPtr(false),
				},
			},
		},
	}

	var serializedConfig []byte
	if Options.IgnitionVersion == "v2" {
		ignc2, err := ignconverter.Translate3to2(config)
		if err != nil {
			return err
		}
		buf, err := json.Marshal(ignc2)
		if err != nil {
			return err
		}
		serializedConfig = buf
	} else {
		buf, err := json.Marshal(config)
		if err != nil {
			return err
		}
		serializedConfig = buf
	}

	plog.Debugf("Creating machine")
	mach, err := c.NewMachine(conf.Ignition(string(serializedConfig)))
	if err != nil {
		return err
	}

	machines := []platform.Machine{mach}
	scpKolet(machines, architecture(pltfrm))
	{
		in, err := os.Open(extbin)
		if err != nil {
			return err
		}
		defer in.Close()
		if err := platform.InstallFile(in, mach, remotepath); err != nil {
			return errors.Wrapf(err, "uploading %s", extbin)
		}
	}

	if extdata != "" {
		if err := platform.CopyDirToMachine(extdata, mach, kolaExtBinDataDir); err != nil {
			return err
		}
	}

	plog.Debugf("Running kolet")
	_, stderr, err := mach.SSH(fmt.Sprintf("sudo ./kolet run-test-unit %s", shellquote.Join(unitname)))
	out, _, suberr := mach.SSH(fmt.Sprintf("sudo systemctl status %s", shellquote.Join(unitname)))
	if suberr != nil {
		fmt.Printf("systemctl status %s:\n%s\n", unitname, string(out))
	}
	if err != nil {
		if Options.SSHOnTestFailure {
			plog.Errorf("dropping to shell: kolet failed: %v: %s", err, stderr)
			platform.Manhole(mach)
		} else {
			return errors.Wrapf(err, "kolet failed: %s", stderr)
		}
	}

	return nil
}
