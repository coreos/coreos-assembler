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

package signature

import (
	"crypto"
	"crypto/rsa"
	_ "crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"hash"

	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/coreos/pkg/capnslog"

	"github.com/coreos/mantle/update/metadata"
)

const (
	signatureVersion = 2
	signatureHash    = crypto.SHA256
	developerPubKey  = `
-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAzFS5uVJ+pgibcFLD3kbY
k02Edj0HXq31ZT/Bva1sLp3Ysv+QTv/ezjf0gGFfASdgpz6G+zTipS9AIrQr0yFR
+tdp1ZsHLGxVwvUoXFftdapqlyj8uQcWjjbN7qJsZu0Ett/qo93hQ5nHW7Sv5dRm
/ZsDFqk2Uvyaoef4bF9r03wYpZq7K3oALZ2smETv+A5600mj1Xg5M52QFU67UHls
EFkZphrGjiqiCdp9AAbAvE7a5rFcJf86YR73QX08K8BX7OMzkn3DsqdnWvLB3l3W
6kvIuP+75SrMNeYAcU8PI1+bzLcAG3VN3jA78zeKALgynUNH50mxuiiU3DO4DZ+p
5QIDAQAB
-----END PUBLIC KEY-----`
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "update/signature")
)

func NewSignatureHash() hash.Hash {
	return signatureHash.New()
}

func VerifySignature(sum []byte, sigs *metadata.Signatures) error {
	pemBlock, _ := pem.Decode([]byte(developerPubKey))
	if pemBlock == nil {
		return fmt.Errorf("unable to parse key")
	}

	somePub, err := x509.ParsePKIXPublicKey(pemBlock.Bytes)
	if err != nil {
		return err
	}

	rsaPub, ok := somePub.(*rsa.PublicKey)
	if !ok {
		return fmt.Errorf("unexpected key type %T", somePub)
	}

	for _, sig := range sigs.Signatures {
		v := sig.GetVersion()
		if v != signatureVersion {
			plog.Debugf("Skipping v%d signature", v)
			continue
		}

		if err := rsa.VerifyPKCS1v15(rsaPub, signatureHash, sum, sig.Data); err != nil {
			plog.Debugf("Cannot verify v%d signature with dev key", v)
		} else {
			plog.Infof("Good v%d signature by dev key", v)
			return nil
		}

	}

	return fmt.Errorf("no valid signatures found")
}
