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
	"crypto/rand"
	"crypto/rsa"
	_ "crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"hash"

	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/coreos/pkg/capnslog"
	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/golang/protobuf/proto"

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
	developerSecKey = `
-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEAzFS5uVJ+pgibcFLD3kbYk02Edj0HXq31ZT/Bva1sLp3Ysv+Q
Tv/ezjf0gGFfASdgpz6G+zTipS9AIrQr0yFR+tdp1ZsHLGxVwvUoXFftdapqlyj8
uQcWjjbN7qJsZu0Ett/qo93hQ5nHW7Sv5dRm/ZsDFqk2Uvyaoef4bF9r03wYpZq7
K3oALZ2smETv+A5600mj1Xg5M52QFU67UHlsEFkZphrGjiqiCdp9AAbAvE7a5rFc
Jf86YR73QX08K8BX7OMzkn3DsqdnWvLB3l3W6kvIuP+75SrMNeYAcU8PI1+bzLcA
G3VN3jA78zeKALgynUNH50mxuiiU3DO4DZ+p5QIDAQABAoIBAH7ENbE+9+nkPyMx
hekaBPVmSz7b3/2iaTNWmckmlY5aSX3LxejtH3rLBjq7rihWGMXJqg6hodcfeGfP
Zb0H2AeKq1Nlac7qq05XsKGRv3WXs6dyO1BDkH/Minh5dk1o0NrwEm91kXLSLfe8
IsCwxPCjwgfGFTjpFLpL4zjA/nFmWRyk2eyvs5VYRGKbbC83alUy7LutyRdZfw1b
nwXldw2m8k/HPbGhaAqPpXTOjckIXZS5Dcp3smrOzwObZ6c3gQzg8upaRmxJVOmk
cgCFTe0yUB2GMTEE3SUmuWJyZqECoyQtuiu0yT3igH8MZQpjg9NXm0eho/bXjN36
frH+ikUCgYEA7VdCRcisnYWct29j+Bnaio9yXwwxhfoee53a4LQgjw5RLGUe1mXe
j56oZ1Mak3Hh55sVQLNXZBuXHQqPsr7KkWXJXedDNFfq1u6by4LeJV0YYiDjjaCM
T5G4Tcs7xhBWszLMCjhpJCrwHdGk3aa65UQ+angZlxhyziULCjpb5rMCgYEA3GUb
VkqlVuNkHoogOMwg+h1jUSkwtWvP/z/FOXrKjivuwSgQ+i6PsildI3FL/WQtJxgd
arB+l0L8TZJ6spFdNXwGmdCLqEcgEBYl11EojOXYLa7oLONI41iRQ3/nBBIqC38P
Cs6CZQG/ZpKSoOzXE34BwcrOL99MA2oaVpGHuQcCgYA1IIk3Mbph8FyqOwb3rGHd
Dksdt48GXHyiUy2BixCWtS+6blA+0cLGB0/PAS07wAw/WdmiCAMR55Ml7w1Hh6m0
bkJrAK9schmhTvwUzBCJ8JLatF37f+qojQfichHJPjMKHd7KkuIGNI5XPmxXKVFA
rMwD7SpdRh28w1H7UiDsPQKBgGebnFtXohyTr2hv9K/evo32LM9ltsFC2rga6YOZ
BwoI+yeQx1JleyX9LgzQYTHQ2y0quAGE0S4YznVFLCswDQpssMm0cUL9lMQbNVTg
kViTYKoxNHKNsqE17Kw3v4l5ZIydAZxJ8qC7TphQxV+jl4RRU1AgIAf/SEO+qH0T
0yMXAoGBAN+y9QpGnGX6cgwLQQ7IC6MC+3NRed21s+KxHzpyF+Zh/q6NTLUSgp8H
dBmeF4wAZTY+g/fdB9drYeaSdRs3SZsM7gMEvjspjYgE2rV/5gkncFyGKRAiNOR4
bsy1Gm/UYLTc8+S3fq/xjg9RCjW9JMwavAwL6oVNNt7nyAXPfvSu
-----END RSA PRIVATE KEY-----
`
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "update/signature")
)

func NewSignatureHash() hash.Hash {
	return signatureHash.New()
}

func keySize() (int, error) {
	pemBlock, _ := pem.Decode([]byte(developerPubKey))
	if pemBlock == nil {
		return 0, fmt.Errorf("unable to parse key")
	}

	somePub, err := x509.ParsePKIXPublicKey(pemBlock.Bytes)
	if err != nil {
		return 0, err
	}

	rsaPub, ok := somePub.(*rsa.PublicKey)
	if !ok {
		return 0, fmt.Errorf("unexpected key type %T", somePub)
	}

	// This is how the rsa package computes key length.
	return (rsaPub.N.BitLen() + 7) / 8, nil
}

func SignaturesSize() (int, error) {
	dataLen, err := keySize()
	if err != nil {
		return 0, err
	}
	data := make([]byte, dataLen)
	sigs := &metadata.Signatures{
		Signatures: []*metadata.Signatures_Signature{
			&metadata.Signatures_Signature{
				Version: proto.Uint32(signatureVersion),
				Data:    data,
			},
		},
	}
	return proto.Size(sigs), nil
}

func Sign(sum []byte) (*metadata.Signatures, error) {
	pemBlock, _ := pem.Decode([]byte(developerSecKey))
	if pemBlock == nil {
		return nil, fmt.Errorf("unable to parse key")
	}

	rsaKey, err := x509.ParsePKCS1PrivateKey(pemBlock.Bytes)
	if err != nil {
		return nil, err
	}

	sig, err := rsa.SignPKCS1v15(rand.Reader, rsaKey, signatureHash, sum)
	if err != nil {
		return nil, err
	}

	return &metadata.Signatures{
		Signatures: []*metadata.Signatures_Signature{
			&metadata.Signatures_Signature{
				Version: proto.Uint32(signatureVersion),
				Data:    sig,
			},
		},
	}, nil
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
