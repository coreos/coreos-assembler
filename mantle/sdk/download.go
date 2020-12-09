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
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/coreos/pkg/capnslog"
	"google.golang.org/api/option"
	"google.golang.org/api/storage/v1"

	"github.com/coreos/mantle/util"
)

var plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "sdk")

func DownloadFile(file, fileURL string, client *http.Client) error {
	plog.Infof("Downloading %s to %s", fileURL, file)

	// handle bucket urls by using api to get media link
	parseURL, err := url.Parse(fileURL)
	if err != nil {
		return err
	}
	resolveGS := func() error {
		if client == nil {
			client = http.DefaultClient
		}
		api, err := storage.NewService(context.Background(), option.WithHTTPClient(client))
		if err != nil {
			return err
		}
		path := strings.TrimLeft(parseURL.Path, "/")
		obj, err := api.Objects.Get(parseURL.Host, path).Do()
		if err != nil {
			return fmt.Errorf("%s: %s", err, fileURL)
		}
		fileURL = obj.MediaLink
		return nil
	}
	if parseURL.Scheme == "gs" {
		if err := util.Retry(5, 1*time.Second, resolveGS); err != nil {
			return err
		}
	}

	if err := os.MkdirAll(filepath.Dir(file), 0777); err != nil {
		return err
	}

	download := func() error {
		return downloadFile(file, fileURL, client)
	}
	if err := util.Retry(5, 1*time.Second, download); err != nil {
		return err
	}
	return nil
}

func downloadFile(file, url string, client *http.Client) error {
	if client == nil {
		client = http.DefaultClient
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

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var length int64
	switch resp.StatusCode {
	case http.StatusOK:
		if pos != 0 {
			if _, err := dst.Seek(0, io.SeekStart); err != nil {
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

		if _, err := dst.Seek(pos, io.SeekStart); err != nil {
			return err
		}
		plog.Infof("Resuming from byte %d", pos)
	case http.StatusRequestedRangeNotSatisfiable:
		plog.Infof("Download already complete")
		return nil
	default:
		return fmt.Errorf("%s: %s", resp.Status, resp.Request.URL)
	}

	prefix := filepath.Base(file)
	if n, err := util.CopyProgress(capnslog.INFO, prefix, dst, resp.Body, resp.ContentLength); err != nil {
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

func DownloadSignedFile(file, url string, client *http.Client, verifyKeyFile string) error {

	if _, err := os.Stat(file + ".sig"); err == nil {
		if e := VerifyFile(file, verifyKeyFile); e == nil {
			plog.Infof("Verified existing file: %s", file)
			return nil
		}
	}

	if err := DownloadFile(file, url, client); err != nil {
		return err
	}

	if err := DownloadFile(file+".sig", url+".sig", client); err != nil {
		return err
	}

	if err := VerifyFile(file, verifyKeyFile); err != nil {
		return err
	}

	plog.Infof("Verified file: %s", file)
	return nil
}

// false if both files do not exist
func cmpFileBytes(file1, file2 string) (bool, error) {
	info1, err := os.Stat(file1)
	if err != nil {
		return false, err
	}
	info2, err := os.Stat(file2)
	if err != nil {
		return false, err
	}
	if info1.Size() != info2.Size() {
		return false, nil
	}

	f1, err := os.Open(file1)
	if err != nil {
		return false, err
	}
	defer f1.Close()
	f2, err := os.Open(file2)
	if err != nil {
		return false, err
	}
	defer f2.Close()

	const defaultBufSize = 4096 // same as bufio
	buf1 := make([]byte, defaultBufSize)
	buf2 := make([]byte, defaultBufSize)

	for {
		n1, err1 := io.ReadFull(f1, buf1)
		n2, err2 := io.ReadFull(f2, buf2)

		if err1 == io.EOF && err2 == io.EOF {
			return true, nil
		} else if err1 == io.EOF || err2 == io.EOF {
			return false, nil
		}

		if err1 == io.ErrUnexpectedEOF && err2 == io.ErrUnexpectedEOF {
			return bytes.Equal(buf1[:n1], buf2[:n2]), nil
		} else if err1 == io.ErrUnexpectedEOF || err2 == io.ErrUnexpectedEOF {
			return false, nil
		}

		if err1 != nil {
			return false, err1
		}
		if err2 != nil {
			return false, err2
		}

		if !bytes.Equal(buf1, buf2) {
			return false, nil
		}
	}
}

// UpdateFile downloads a file to temp dir and replaces the file only if
// contents have changed. If tempDir is "" default will be os.TempDir().
// Leave client nil to use default.
func UpdateFile(file, url string, client *http.Client) error {
	if err := os.MkdirAll(filepath.Dir(file), 0777); err != nil {
		return err
	}

	tempFile := file + ".part"
	if err := DownloadFile(tempFile, url, client); err != nil {
		return fmt.Errorf("%s: %s", url, err)
	}
	defer os.Remove(tempFile)

	equal, err := cmpFileBytes(file, tempFile)
	if os.IsExist(err) { // file may not exist, that is ok
		return err
	}
	if equal {
		plog.Infof("%v is up to date", file)
		return nil
	}

	// not equal so delete any existing file and rename tempFile to file
	if err := os.Rename(tempFile, file); err != nil {
		return err
	}
	return nil
}

// UpdateSignedFile will download and replace the local file if the
// published signature doesn't match the local copy. Leave client nil to
// use default.
func UpdateSignedFile(file, url string, client *http.Client, verifyKeyFile string) error {
	sigFile := file + ".sig"
	sigURL := url + ".sig"

	// update local sig to latest
	if err := UpdateFile(sigFile, sigURL, client); err != nil {
		return err
	}

	// try to verify with latest sig
	if e := VerifyFile(file, verifyKeyFile); e == nil {
		plog.Infof("Verified existing file: %s", file)
		return nil
	}

	// download image and try to verify again
	if err := UpdateFile(file, url, client); err != nil {
		return err
	}
	if err := VerifyFile(file, verifyKeyFile); err != nil {
		return err
	}

	plog.Infof("Verified file: %s", file)
	return nil
}
