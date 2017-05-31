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
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
)

var (
	PackageHashMismatchError = errors.New("package hash is invalid")
	PackageSizeMismatchError = errors.New("package size is invalid")
)

// Package represents a single downloadable file.
type Package struct {
	Name     string `xml:"name,attr"`
	SHA1     string `xml:"hash,attr"`
	SHA256   string `xml:"hash_sha256,attr,omitempty"`
	Size     uint64 `xml:"size,attr"`
	Required bool   `xml:"required,attr"`
}

func (p *Package) FromPath(name string) error {
	f, err := os.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()

	err = p.FromReader(f)
	if err != nil {
		return err
	}

	p.Name = filepath.Base(name)
	return nil
}

func (p *Package) FromReader(r io.Reader) error {
	sha1b64, sha256b64, n, err := multihash(r)
	if err != nil {
		return err
	}

	p.SHA1 = sha1b64
	p.SHA256 = sha256b64
	p.Size = uint64(n)
	return nil
}

func (p *Package) Verify(dir string) error {
	f, err := os.Open(filepath.Join(dir, p.Name))
	if err != nil {
		return err
	}
	defer f.Close()

	return p.VerifyReader(f)
}

func (p *Package) VerifyReader(r io.Reader) error {
	sha1b64, sha256b64, n, err := multihash(r)
	if err != nil {
		return err
	}

	if p.Size != uint64(n) {
		return PackageSizeMismatchError
	}

	if p.SHA1 != sha1b64 {
		return PackageHashMismatchError
	}

	// Allow SHA256 to be empty since it is a later protocol addition.
	if p.SHA256 != "" && p.SHA256 != sha256b64 {
		return PackageHashMismatchError
	}

	return nil
}

func multihash(r io.Reader) (sha1b64, sha256b64 string, n int64, err error) {
	h1 := sha1.New()
	h256 := sha256.New()
	if n, err = io.Copy(io.MultiWriter(h1, h256), r); err != nil {
		return
	}

	sha1b64 = base64.StdEncoding.EncodeToString(h1.Sum(nil))
	sha256b64 = base64.StdEncoding.EncodeToString(h256.Sum(nil))
	return
}
