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
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/openpgp"
	"golang.org/x/crypto/openpgp/armor"
	"golang.org/x/crypto/openpgp/packet"
)

const rpmgpgKeyring = "/etc/pki/rpm-gpg"

func Verify(signed, signature io.Reader, verifyKeyFile string) error {
	var err error
	var keyring openpgp.EntityList
	if verifyKeyFile == "" {
		keyring, err = generateKeyRingFromDir(rpmgpgKeyring)
		if err != nil {
			return err
		}
	} else {
		b, err := ioutil.ReadFile(verifyKeyFile)
		if err != nil {
			return fmt.Errorf("%v: %s", err, verifyKeyFile)
		}
		keyring, err = openpgp.ReadArmoredKeyRing(strings.NewReader(string(b[:])))
		if err != nil {
			return err
		}
	}

	_, err = openpgp.CheckDetachedSignature(keyring, signed, signature)
	return err
}

func VerifyFile(file, verifyKeyFile string) error {
	signed, err := os.Open(file)
	if err != nil {
		return err
	}
	defer signed.Close()

	signature, err := os.Open(file + ".sig")
	if err != nil {
		return err
	}
	defer signature.Close()

	return Verify(signed, signature, verifyKeyFile)
}

func generateKeyRingFromDir(dir string) (openpgp.EntityList, error) {
	var keyring openpgp.EntityList

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		f, err := os.OpenFile(path, os.O_RDONLY, 0)
		if err != nil {
			return err
		}
		defer f.Close()

		block, err := armor.Decode(f)
		if err != nil {
			return err
		}

		if block.Type != openpgp.PublicKeyType {
			return nil
		}

		e, err := openpgp.ReadEntity(packet.NewReader(block.Body))
		if err != nil {
			return err
		}

		keyring = append(keyring, e)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return keyring, nil
}
