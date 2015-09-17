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
	"bytes"
	"crypto/sha512"
	"fmt"
	"io"
	"io/ioutil"
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

func fileSum(file string) ([]byte, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	hash := sha512.New()
	if _, err := io.Copy(hash, f); err != nil {
		return nil, err
	}

	return hash.Sum(nil), nil
}

func compareFileBytes(fileName1, fileName2 string) (bool, error) {
	sum1, err := fileSum(fileName1)
	if err != nil {
		return false, err
	}
	sum2, err := fileSum(fileName2)
	if err != nil {
		return false, err
	}
	return bytes.Equal(sum1, sum2), nil
}

// UpdateFile downloads a file to temp dir and replaces the file only if
// contents have changed. If tempDir is "" default will be os.TempDir().
func UpdateFile(file, url string) error {
	t, err := ioutil.TempFile(filepath.Dir(file), "sdkUpdateCheck")
	if err != nil {
		return nil
	}
	t.Close()
	tempFile := t.Name()
	defer os.Remove(tempFile)

	// NOTE: It'd be nice to have a Download(dst io.Writer, url) error
	// function so you can just io.Multiwriter() the hasher and file
	// before passing it to Download().
	if err := DownloadFile(tempFile, url); err != nil {
		return err
	}

	equal, err := compareFileBytes(file, tempFile)
	if os.IsExist(err) { // file may not exist, that is ok
		return err
	}
	if equal {
		plog.Infof("%v is up to date", file)
		return nil
	}

	// not equal so delete any existing file and rename tempFile to file
	if err := os.MkdirAll(filepath.Dir(file), 0777); err != nil {
		return err
	}
	if err := os.Rename(tempFile, file); err != nil {
		return err
	}
	return nil
}

// UpdateSignedFile will download and replace the local file if the
// published signature doesn't match the local copy
func UpdateSignedFile(file, url string) error {
	sigFile := file + ".sig"
	sigURL := url + ".sig"

	// update local sig to latest
	if err := UpdateFile(sigFile, sigURL); err != nil {
		return err
	}

	// try to verify with latest sig
	if e := VerifyFile(file); e == nil {
		plog.Infof("Verified existing file: %s", file)
		return nil
	}

	// download image and try to verify again
	if err := UpdateFile(file, url); err != nil {
		return err
	}
	if err := VerifyFile(file); err != nil {
		return err
	}

	plog.Infof("Verified file: %s", file)
	return nil
}
