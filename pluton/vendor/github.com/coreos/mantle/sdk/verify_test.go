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
	"encoding/base64"
	"io"
	"strings"
	"testing"

	"golang.org/x/crypto/openpgp/errors"
)

const (
	versionTxt = `COREOS_BUILD=723
COREOS_BRANCH=1
COREOS_PATCH=0
COREOS_VERSION=723.1.0
COREOS_VERSION_ID=723.1.0
COREOS_BUILD_ID=""
COREOS_SDK_VERSION=717.0.0
`
	versionSig = `
iQIcBAABAgAGBQJVjgCWAAoJEKWpZjXlZ278G/kQAKSqkurFrKywkPhCe3VejSUp
GSS2MmHT4UAhHzopof33eV1mwI7NxPP7oDOeg5ovLxiHbawo/fHYUI9Wt2r9ZUCB
QxXt1fk9yBbUlVd6vdsrLmUZpVNfFmnxUL8iurRJczgSKmqyxbk+HcD+fidkgSAU
5xpLYEfCp1VSZ61a+3NZO8NHval4x3+AYZXOBNqfWz1s7Sewmvm/YbIs0BlwZxrY
CUiYzuNCgDl0qZLRx8C2EmnHk675XvN4Nr0xAHRsARIXfgFR1AVSqVdvzW2ZW3Bq
KBNiF0zfhZ2cdG6Rj9Dp39+skazUYW8bzn1fr374prSALef/WZIAUkLWvUpPdEli
ZnQr9Ufm+ZW2XM+Nm/Ks5Zf5f+0axHESF1ANSKNM7gp7a6+cbXLniXQKUxMLlTGL
TCz29ZdK1M8Wx2V8bisOk24yneOJyVzn5jSO4zCr6xWxBH8yf0B6UQnXWK8fhVDR
V/mehjhms7/8xCfRlTo42h69UqCzp/ZMlJZOTikw4Q7yZwhAu1bERlOWUVSHik8W
UjVp1b0FyuBYEJA3ht2QuIdf54M3bKGsFGMUB0/ro6sm00UF3pjVkG+a7WEU4zpp
nhqSIf7YIqso/oohdpmc338F7G3RgfYoZ2+THXGTxpIvMvkEqxCaKPsprXLZQd94
ULECDto3pYB5cT5A/blA
=/zkZ
`
)

func b64reader(s string) io.Reader {
	return base64.NewDecoder(base64.StdEncoding, strings.NewReader(s))
}

func TestValidSig(t *testing.T) {
	err := Verify(strings.NewReader(versionTxt), b64reader(versionSig))
	if err != nil {
		t.Errorf("Verify failed: %v", err)
	}
}

func TestInvalidSig(t *testing.T) {
	err := Verify(strings.NewReader(versionTxt+"bad"), b64reader(versionSig))
	if err == nil {
		t.Errorf("Verify failed to report bad signature")
	} else if _, ok := err.(errors.SignatureError); !ok {
		t.Errorf("Verify failed: %v", err)
	}
}
