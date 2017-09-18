// Copyright 2017 CoreOS, Inc.
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
//
// Based on https://github.com/hawkowl/httpsig_cffi
//
// Copyright (c) 2014 Adam Knight
// Copyright (c) 2012 Adam T. Lindsay (original author)
//
// Permission is hereby granted, free of charge, to any person obtaining a
// copy of this software and associated documentation files (the "Software"),
// to deal in the Software without restriction, including without limitation
// the rights to use, copy, modify, merge, publish, distribute, sublicense,
// and/or sell copies of the Software, and to permit persons to whom the
// Software is furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL
// THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
// FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER
// DEALINGS IN THE SOFTWARE.

package oci

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var (
	genericHeaders = []string{
		"date",
		"(request-target)",
		"host",
	}

	bodyHeaders = []string{
		"date",
		"(request-target)",
		"host",
		"content-length",
		"content-type",
		"x-content-sha256",
	}

	requiredHeaders = map[string][]string{
		"get":    genericHeaders,
		"head":   genericHeaders,
		"delete": genericHeaders,
		"put":    bodyHeaders,
		"post":   bodyHeaders,
	}
)

type Signer struct {
	key *rsa.PrivateKey

	// <tenancy>/<user>/<rsa fingerprint>
	keyID string
}

func NewSigner(privateKey *rsa.PrivateKey, keyID string) *Signer {
	return &Signer{
		key:   privateKey,
		keyID: keyID,
	}
}

func (s *Signer) SignRequest(req *http.Request) error {
	reqHeaders, ok := requiredHeaders[strings.ToLower(req.Method)]
	if !ok {
		return fmt.Errorf("unknown method")
	}

	err := s.injectMissingHeaders(req)
	if err != nil {
		return fmt.Errorf("injecting headers: %v", err)
	}

	signable, err := s.generateMessage(reqHeaders, req)
	if err != nil {
		return fmt.Errorf("generating signature message: %v", err)
	}

	signature, err := s.sign(signable)
	if err != nil {
		return fmt.Errorf("signing message: %v", err)
	}

	authorization := s.buildAuthorization(reqHeaders, signature)
	req.Header.Set("Authorization", authorization)

	return nil
}

func (s *Signer) injectMissingHeaders(req *http.Request) error {
	if req.Header.Get("date") == "" {
		GMT, _ := time.LoadLocation("")
		now := time.Now().In(GMT)
		req.Header.Set("date", now.Format(time.RFC1123))
	}

	if req.Header.Get("host") == "" {
		req.Header.Set("host", req.URL.Host)
	}

	if req.Header.Get("content-type") == "" {
		req.Header.Set("content-type", "application/json")
	}

	verb := strings.ToLower(req.Method)
	if (verb == "post" || verb == "put") && req.Body != nil {
		body, err := ioutil.ReadAll(req.Body)
		if err != nil {
			return fmt.Errorf("reading body: %v", err)
		}

		if req.Header.Get("x-content-sha256") == "" {
			m := sha256.Sum256([]byte(body))
			req.Header.Set("x-content-sha256", base64.StdEncoding.EncodeToString(m[:]))
		}

		req.Header.Set("content-length", strconv.Itoa(len(body)))

		req.Body = ioutil.NopCloser(bytes.NewReader(body))
	}

	return nil
}

func (s *Signer) buildAuthorization(reqHeaders []string, signature string) string {
	params := map[string]string{
		"keyId":     s.keyID,
		"algorithm": "rsa-sha256",
		"signature": signature,
		"version":   "1",
		"headers":   strings.ToLower(strings.Join(reqHeaders, " ")),
	}

	var kvs []string
	for key, val := range params {
		kvs = append(kvs, fmt.Sprintf("%s=\"%s\"", key, val))
	}

	return fmt.Sprintf("Signature %s", strings.Join(kvs, ","))
}

func (s *Signer) sign(signable string) (string, error) {
	hashed := sha256.Sum256([]byte(signable))
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.key, crypto.SHA256, hashed[:])
	if err != nil {
		return "", fmt.Errorf("signing message: %v", err)
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

func (s *Signer) generateMessage(reqHeaders []string, req *http.Request) (string, error) {
	var signableList []string

	for _, rh := range reqHeaders {
		rh = strings.ToLower(rh)

		switch rh {
		case "(request-target)":
			if req.Method == "" || req.URL.Path == "" {
				return "", fmt.Errorf("method and path are required arguments when using (request-target)")
			}
			path := req.URL.Path
			if req.URL.RawQuery != "" {
				path = fmt.Sprintf("%s?%s", path, req.URL.RawQuery)
			}
			signableList = append(signableList, fmt.Sprintf("%s: %s %s", rh, strings.ToLower(req.Method), path))
		case "host":
			if req.Header.Get(rh) == "" {
				req.Header.Set(rh, req.URL.Host)
			}
			signableList = append(signableList, fmt.Sprintf("%s: %s", rh, req.Header.Get(rh)))
		default:
			h := req.Header.Get(rh)
			if h == "" {
				return "", fmt.Errorf("missing required header: %s", rh)
			}
			signableList = append(signableList, fmt.Sprintf("%s: %s", rh, h))
		}
	}

	return strings.Join(signableList, "\n"), nil
}
