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
iQJIBAABCAAyFiEE/ZhvsJZIL5BvVbLqAcnK52ezyg4FAlxYlhEUHGJ1aWxkYm90QGNvcmVvcy5j
b20ACgkQAcnK52ezyg4hPQ//TmUPPNrKMPgtzlLDzaTjMEoTKzRjTQK1WDCBmfjni/gA+omhVeY0
oBcCzjdULL6BKScC8gzFA2sUTJTTnKTBjEZwYZ82X9ruQVxvis0UQi0G10YY51Fl4zFJyb+cRPE/
QDM5M60Bse41/a2Z0tXthvUyy1+Y++8N/b9q1GZCgKjsCoKG/Pl/5Hz9BvhtfVxe5JPawqrQ3/NI
SC8sZoggA/lMXN7bcvj9sIy3x05Of+851Qc6rAgLIgnEhxHtUTU8k8Y6IUwjCErK8KkuUID+HtxB
+4HsIUflL24y5a1m1VNy7k470EhhxQZThhmx1SHHmCdayzGgsM5azNVQKVTey6AVus4W6D7paLzd
t9heWvZFQ4vjkXnFcEcUwAIlIgN9a0jGJJrAzVg/bcoMVP45g+Lg0qQQnpkHXq0+vtuLFaEHcDQF
UJ6S6Isb5M+3w6ZGQQK+WF2Vx5QqA5XWJAKbuhA6gzqCaOgKz82sxfS0fNp3gcnMeIYYBmW/YpJX
f9B3bDASEJvnU0qd9y72LsayXnoECsF/NOtCjT6j6n3WxWdn+5s+8/tSml9K+zUzFat5+fuPZuoI
rgt9DnN4qxtAcSexsIseueFYezXmwIGah6IT+PzjMX6xCB2P4FK63eMykS0jjJxqYFRDBH/VPZ59
HxAtMttP51U0Vk7lXqD66MU=
`
)

func b64reader(s string) io.Reader {
	return base64.NewDecoder(base64.StdEncoding, strings.NewReader(s))
}

func TestValidSig(t *testing.T) {
	err := Verify(strings.NewReader(versionTxt), b64reader(versionSig), buildbot_coreos_PubKey)
	if err != nil {
		t.Errorf("Verify failed: %v", err)
	}
}

func TestInvalidSig(t *testing.T) {
	err := Verify(strings.NewReader(versionTxt+"bad"), b64reader(versionSig), buildbot_coreos_PubKey)
	if err == nil {
		t.Errorf("Verify failed to report bad signature")
	} else if _, ok := err.(errors.SignatureError); !ok {
		t.Errorf("Verify failed: %v", err)
	}
}
