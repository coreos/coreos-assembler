// Copyright 2026 Red Hat, Inc.
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

package kola

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMetadataFromTestBinary(t *testing.T) {
	tests := []struct {
		name     string
		contents string
		want     *externalTestMeta
	}{
		{
			name:     "no inline metadata",
			contents: "#!/bin/bash\necho no-metadata\n",
			want:     nil,
		},
		{
			name:     "JSON metadata defaults to exclusive",
			contents: "#!/bin/bash\n# kola: {\"tags\":\"json\",\"minMemory\":1024}\necho json\n",
			want: &externalTestMeta{
				Tags:      "json",
				MinMemory: 1024,
				Exclusive: true,
			},
		},
		{
			name: "YAML metadata defaults to exclusive",
			contents: "#!/bin/bash\n" +
				"## kola:\n" +
				"## tags: yaml\n" +
				"## minMemory: 2048\n" +
				"echo yaml\n",
			want: &externalTestMeta{
				Tags:      "yaml",
				MinMemory: 2048,
				Exclusive: true,
			},
		},
		{
			name:     "JSON metadata can disable exclusivity",
			contents: "#!/bin/bash\n# kola: {\"exclusive\":false}\necho json\n",
			want: &externalTestMeta{
				Exclusive: false,
			},
		},
		{
			name: "YAML metadata can disable exclusivity",
			contents: "#!/bin/bash\n" +
				"## kola:\n" +
				"## exclusive: false\n" +
				"echo yaml\n",
			want: &externalTestMeta{
				Exclusive: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executable := filepath.Join(t.TempDir(), "test.sh")
			if err := os.WriteFile(executable, []byte(tt.contents), 0755); err != nil {
				t.Fatal(err)
			}

			got, err := metadataFromTestBinary(executable)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("metadataFromTestBinary() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
