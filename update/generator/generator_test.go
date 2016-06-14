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
	"io/ioutil"
	"os"
	"testing"

	"github.com/coreos/mantle/update"
)

type testGenerator struct {
	Generator
	t *testing.T
}

// Report errors to testing framework instead of return value.
func (g *testGenerator) Destroy() error {
	if err := g.Generator.Destroy(); err != nil {
		g.t.Errorf("Generator.Destroy: %v", err)
	}
	return nil
}

func TestGenerateWithoutPartition(t *testing.T) {
	g := testGenerator{t: t}
	defer g.Destroy()

	f, err := ioutil.TempFile("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	defer os.Remove(f.Name())

	if err := g.Write(f.Name()); err != nil {
		t.Fatal(err)
	}

	if _, err := f.Seek(0, os.SEEK_SET); err != nil {
		t.Fatal(err)
	}

	payload, err := update.NewPayloadFrom(f)
	if err != nil {
		t.Fatal(err)
	}

	if err := payload.Verify(); err != nil {
		t.Fatal(err)
	}
}
