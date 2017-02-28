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
	"testing"
)

func TestEmptyInstallInfo(t *testing.T) {
	info, err := NewInstallInfo(bytes.NewReader([]byte{}))
	if err != nil {
		t.Fatal(err)
	}

	if info.Size == nil {
		t.Error("InstallInfo.Size is nil")
	} else if *info.Size != 0 {
		t.Errorf("InstallInfo.Size should be 0, got %d", *info.Size)
	}

	if !bytes.Equal(info.Hash, testEmptyHash) {
		t.Errorf("InstallInfo.Hash should be %q, got %q", testEmptyHash, info.Hash)
	}
}

func TestOnesInstallInfo(t *testing.T) {
	info, err := NewInstallInfo(bytes.NewReader(testOnes))
	if err != nil {
		t.Fatal(err)
	}

	if info.Size == nil {
		t.Error("InstallInfo.Size is nil")
	} else if *info.Size != BlockSize {
		t.Errorf("InstallInfo.Size should be %d, got %d", BlockSize, *info.Size)
	}

	if !bytes.Equal(info.Hash, testOnesHash) {
		t.Errorf("InstallInfo.Hash should be %q, got %q", testOnesHash, info.Hash)
	}
}

func TestUnalignedInstallInfo(t *testing.T) {
	info, err := NewInstallInfo(bytes.NewReader(testUnaligned))
	if err != nil {
		t.Fatal(err)
	}

	if info.Size == nil {
		t.Error("InstallInfo.Size is nil")
	} else if *info.Size != BlockSize+1 {
		t.Errorf("InstallInfo.Size should be %d, got %d", BlockSize, *info.Size)
	}

	if !bytes.Equal(info.Hash, testUnalignedHash) {
		t.Errorf("InstallInfo.Hash should be %q, got %q", testUnalignedHash, info.Hash)
	}
}
