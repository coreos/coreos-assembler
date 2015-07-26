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

func TarballName(version string) string {
	return fmt.Sprintf("coreos-sdk-%s-%s.tar.bz2", LocalArch(), version)
}

func TarballURL(version string) string {
	p := path.Join(urlPath, LocalArch(), version, TarballName(version))
	u := url.URL{Scheme: "https", Host: urlHost, Path: p}
	return u.String()
}

func DownloadFile(file, url string) error {
	plog.Infof("Downloading %s to %s", url, file)

	if err := os.MkdirAll(filepath.Dir(file), 0777); err != nil {
		return err
	}

	dst, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		return err
	}
	defer dst.Close()

	pos, err := dst.Seek(0, os.SEEK_END)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}

	if pos != 0 {
		req.Header.Add("Range", fmt.Sprintf("bytes=%d-", pos))
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var length int64
	switch resp.StatusCode {
	case http.StatusOK:
		if pos != 0 {
			if _, err := dst.Seek(0, os.SEEK_SET); err != nil {
				return err
			}
			if err := dst.Truncate(0); err != nil {
				return err
			}
			pos = 0
		}
		length = resp.ContentLength
	case http.StatusPartialContent:
		var end int64
		n, _ := fmt.Sscanf(resp.Header.Get("Content-Range"),
			"bytes %d-%d/%d", &pos, &end, &length)
		if n != 3 {
			return fmt.Errorf("Bad Content-Range for %s", resp.Request.URL)
		}

		if _, err := dst.Seek(pos, os.SEEK_SET); err != nil {
			return err
		}
		plog.Infof("Resuming from byte %d", pos)
	case http.StatusRequestedRangeNotSatisfiable:
		plog.Infof("Download already complete")
		return nil
	default:

		return fmt.Errorf("%s: %s", resp.Status, resp.Request.URL)
	}

	// TODO(marineam): log download progress
	if n, err := io.Copy(dst, resp.Body); err != nil {
		return err
	} else if n != length-pos {
		// unsure if this is worth caring about
		plog.Infof("Downloaded %d bytes, expected %d", n, length-pos)
		return nil
	} else {
		plog.Infof("Downloaded %d bytes", n)
		return nil
	}
}

func DownloadSignedFile(file, url string) error {
	if _, err := os.Stat(file + ".sig"); err == nil {
		if e := VerifyFile(file); e == nil {
			plog.Infof("Verified existing file: %s", file)
			return nil
		}
	}

	if err := DownloadFile(file, url); err != nil {
		return err
	}

	if err := DownloadFile(file+".sig", url+".sig"); err != nil {
		return err
	}

	if err := VerifyFile(file); err != nil {
		return err
	}

	plog.Infof("Verified file: %s", file)
	return nil
}

func DownloadSDK(version string) error {
	tarFile := filepath.Join(RepoCache(), "sdk", TarballName(version))
	tarURL := TarballURL(version)
	return DownloadSignedFile(tarFile, tarURL)
}
