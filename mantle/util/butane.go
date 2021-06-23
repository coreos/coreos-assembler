// Copyright 2021 Red Hat, Inc.
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

package util

import (
	"bufio"
	"io"
	"io/ioutil"
	"os"
	"strings"

	"github.com/coreos/butane/config"
	"github.com/coreos/butane/config/common"
)

// ButaneStringToIgniton takes a string and translates it into Ignition data.
func ButaneStringToIgnition(s string) ([]byte, error) {
	r := strings.NewReader(s)
	return ButaneReaderToIgnition(r)
}

// ButaneFileToIgnition reads a file and translates it into Ignition data.
func ButaneFileToIgnition(fname string) ([]byte, error) {
	in, err := os.Open(fname)
	if err != nil {
		return nil, err
	}
	defer in.Close()
	b := bufio.NewReader(in)

	return ButaneReaderToIgnition(b)
}

// ButaneReaderToIgnition takes an io.Reader and translates it into Ignition.
func ButaneReaderToIgnition(r io.Reader) ([]byte, error) {
	data, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}
	out, _, err := config.TranslateBytes(data,
		common.TranslateBytesOptions{
			Pretty: true,
			Strict: true,
		})
	return out, err
}
