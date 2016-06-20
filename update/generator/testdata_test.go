// Copyright 2016 CoreOS, Inc.
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

package generator

import (
	"bytes"
	"encoding/base64"
)

const (
	testEmptyHashStr     = `47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=`
	testOnesHashStr      = `9HqOw+mv8jGNiWlCKCrU/jfWORyCkU9UpdqKN94TAMY=`
	testUnalignedHashStr = `6pwJcxe6bTOSepRIAED1jRKLlIMd+xhzoxv1CzBayrE=`
)

var (
	testEmptyHash     []byte
	testOnes          []byte
	testOnesHash      []byte
	testUnaligned     []byte
	testUnalignedHash []byte
)

func init() {
	testEmptyHash = mustBase64(testEmptyHashStr)
	testOnes = bytes.Repeat([]byte{0xff}, BlockSize)
	testOnesHash = mustBase64(testOnesHashStr)
	testUnaligned = append(testOnes, 0xff)
	testUnalignedHash = mustBase64(testUnalignedHashStr)
}

func mustBase64(s string) []byte {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}
