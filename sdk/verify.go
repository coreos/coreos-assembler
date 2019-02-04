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
	"strings"

	"golang.org/x/crypto/openpgp"
)

// CoreOS image signing key https://coreos.com/security/image-signing-key/
// $ gpg2 --list-keys --list-options show-unusable-subkeys \
//     --keyid-format SHORT 04127D0BFABEC8871FFB2CCE50E0885593D2DCB4
// pub   rsa4096/93D2DCB4 2013-09-06 [SC]
//       04127D0BFABEC8871FFB2CCE50E0885593D2DCB4
// uid         [ unknown] CoreOS Buildbot (Offical Builds) <buildbot@coreos.com>
// sub   rsa4096/74E7E361 2013-09-06 [S] [expired: 2014-09-06]
// sub   rsa4096/E5676EFC 2014-09-08 [S] [expired: 2015-09-08]
// sub   rsa4096/1CB5FA26 2015-08-31 [S] [expired: 2017-08-30]
// sub   rsa4096/B58844F1 2015-11-20 [S] [revoked: 2016-05-16]
// sub   rsa4096/2E16137F 2016-05-16 [S] [expired: 2017-05-16]
// sub   rsa4096/EF4B4ED9 2017-05-22 [S] [expired: 2018-06-01]
// sub   rsa4096/0638EB2F 2018-02-10 [S] [expires: 2019-06-01]
// sub   rsa4096/67B3CA0E 2019-02-04 [S] [expires: 2021-06-01]
const buildbot_coreos_PubKey = `
-----BEGIN PGP PUBLIC KEY BLOCK-----

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
=q4at
-----END PGP PUBLIC KEY BLOCK-----
`

func Verify(signed, signature io.Reader, key string) error {

	keyring, err := openpgp.ReadArmoredKeyRing(strings.NewReader(key))

	if err != nil {
		panic(err)
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

	var key string
	if verifyKeyFile == "" {
		key = buildbot_coreos_PubKey
	} else {
		b, err := ioutil.ReadFile(verifyKeyFile)
		if err != nil {
			return fmt.Errorf("%v: %s", err, verifyKeyFile)
		}
		key = string(b[:])
	}

	if err := Verify(signed, signature, key); err != nil {
		return fmt.Errorf("%v: %s", err, file)
	}
	return nil
}
