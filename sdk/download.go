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

package sdk

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"

	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/coreos/pkg/capnslog"
)

const (
	urlHost = "storage.googleapis.com"
	urlPath = "/builds.developer.core-os.net/sdk"
)

var plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "sdk")

func get(url string) (resp *http.Response, err error) {
	plog.Infof("Fetching %s", url)
	resp, err = http.Get(url)
	if err == nil && resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		resp.Body = nil
		err = fmt.Errorf("%s: %s", resp.Status, resp.Request.URL)
	}
	return
}

func TarballName(version string) string {
	return fmt.Sprintf("coreos-sdk-%s-%s.tar.bz2", LocalArch(), version)
}

func TarballURL(version string) string {
	p := path.Join(urlPath, LocalArch(), version, TarballName(version))
	u := url.URL{Scheme: "https", Host: urlHost, Path: p}
	return u.String()
}

func Download(version string) error {
	sdk := filepath.Join(RepoCache(), "sdk")
	if err := os.MkdirAll(sdk, 0777); err != nil {
		return err
	}

	tar, err := os.OpenFile(filepath.Join(sdk, TarballName(version)),
		os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		return err
	}
	defer tar.Close()

	respTar, err := get(TarballURL(version))
	if err != nil {
		return err
	}
	defer respTar.Body.Close()

	// TODO(marineam): log download progress
	if _, err := io.Copy(tar, respTar.Body); err != nil {
		return err
	}

	if _, err := tar.Seek(0, os.SEEK_SET); err != nil {
		return err
	}

	respSig, err := get(TarballURL(version) + ".sig")
	if err != nil {
		return err
	}
	defer respSig.Body.Close()

	return Verify(tar, respSig.Body)
}
