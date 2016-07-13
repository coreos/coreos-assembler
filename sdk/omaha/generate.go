// Copyright 2015 CoreOS, Inc.
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

package omaha

import (
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/coreos/pkg/capnslog"

	"github.com/coreos/mantle/network/omaha"
	"github.com/coreos/mantle/sdk"
)

const (
	privateKey = "/usr/share/update_engine/update-payload-key.key.pem"
	publicKey  = "/usr/share/update_engine/update-payload-key.pub.pem"
)

var plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "sdk/omaha")

func run(name string, arg ...string) error {
	cmd := exec.Command(name, arg...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func xmlMarshalFile(path string, v interface{}) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.WriteString(xml.Header); err != nil {
		return err
	}

	enc := xml.NewEncoder(f)
	enc.Indent("", "  ")
	return enc.Encode(v)
}

func xmlUnmarshalFile(path string, v interface{}) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	return xml.NewDecoder(f).Decode(v)
}

func checkUpdate(dir, update_xml string) error {
	u := omaha.Update{}
	if err := xmlUnmarshalFile(update_xml, &u); err != nil {
		return err
	}

	if len(u.Packages) != 1 {
		return fmt.Errorf("%s contains %d packages, expected 1",
			update_xml, len(u.Packages))
	}

	pkgdir := filepath.Join(dir, u.URL.CodeBase)
	return u.Packages[0].Verify(pkgdir)
}

func GenerateFullUpdate(dir string) error {
	var (
		update_prefix = filepath.Join(dir, "coreos_production_update")
		update_bin    = update_prefix + ".bin"
		update_gz     = update_prefix + ".gz"
		update_xml    = update_prefix + ".xml"
	)

	if err := checkUpdate(dir, update_xml); err == nil {
		plog.Infof("Using update manifest: %s", update_xml)
		return nil
	}

	plog.Noticef("Generating update payload: %s", update_gz)
	if err := run("delta_generator",
		"-new_image", update_bin,
		"-out_file", update_gz,
		"-private_key", privateKey); err != nil {
		return err
	}

	plog.Infof("Writing update manifest: %s", update_xml)
	update := omaha.Update{Id: sdk.GetDefaultAppId()}
	pkg, err := update.AddPackageFromPath(update_gz)
	if err != nil {
		return err
	}

	// update engine needs the payload hash here in the action element
	postinstall := update.AddAction("postinstall")
	postinstall.Sha256 = pkg.Sha256

	if ver, err := sdk.VersionsFromDir(dir); err != nil {
		return err
	} else {
		update.Version = ver.Version
	}

	return xmlMarshalFile(update_xml, &update)
}
