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
	"io"
	"io/ioutil"
	"os"
	"testing"

	"github.com/coreos/mantle/system"
	"github.com/coreos/mantle/system/exec"
	"github.com/coreos/mantle/update/metadata"
)

func TestFullUpdateScanEmpty(t *testing.T) {
	scanner := fullScanner{
		payload: &bytes.Buffer{},
		source:  bytes.NewReader([]byte{}),
	}

	if err := scanner.Scan(); err != io.EOF {
		t.Errorf("expected io.EOF, got %v", err)
	}

	if scanner.offset != 0 {
		t.Errorf("read %d bytes from nowhere", scanner.offset)
	}

	if len(scanner.operations) != 0 {
		t.Errorf("operations not empty: %v", scanner.operations)
	}
}

func checkReplace(t *testing.T, ops []*metadata.InstallOperation, source, sourceHash, payload []byte) {
	if len(ops) != 1 {
		t.Fatalf("unexpected operations: %v", ops)
	}

	op := ops[0]
	if op.GetType() != metadata.InstallOperation_REPLACE {
		t.Errorf("unexpected operation type: %s", op.GetType())
	}

	if len(op.DstExtents) != 1 {
		t.Fatalf("unexpected extents: %d", op.GetDstExtents())
	}

	ext := op.DstExtents[0]
	if ext.GetStartBlock() != 0 || ext.GetNumBlocks() != 1 {
		t.Fatalf("unexpected extent: %v", ext)
	}

	if op.GetDataLength() != BlockSize {
		t.Errorf("unexpected payload size %d", op.GetDataLength())
	}

	if !bytes.Equal(op.DataSha256Hash, sourceHash) {
		t.Error("unexpected payload hash")
	}

	if !bytes.Equal(payload, source) {
		t.Errorf("source not coppied to payload")
	}
}

func checkReplaceBZ(t *testing.T, ops []*metadata.InstallOperation, source, sourceHash, payload []byte) {
	if len(ops) != 1 {
		t.Fatalf("unexpected operations: %v", ops)
	}

	op := ops[0]
	if op.GetType() != metadata.InstallOperation_REPLACE_BZ {
		t.Errorf("unexpected operation type: %s", op.GetType())
	}

	if len(op.DstExtents) != 1 {
		t.Fatalf("unexpected extents: %d", op.GetDstExtents())
	}

	ext := op.DstExtents[0]
	if ext.GetStartBlock() != 0 || ext.GetNumBlocks() != 1 {
		t.Fatalf("unexpected extent: %v", ext)
	}

	if op.GetDataLength() == 0 && op.GetDataLength() >= BlockSize {
		t.Errorf("unexpected payload size %d", op.GetDataLength())
	}

	if !bytes.Equal(bunzip2(t, payload), source) {
		t.Errorf("source not properly compressed in payload")
	}
}

func checkFullScan(t *testing.T, source []byte) ([]*metadata.InstallOperation, []byte) {
	var payload bytes.Buffer
	scanner := fullScanner{
		payload: &payload,
		source:  bytes.NewReader(source),
	}

	if err := scanner.Scan(); err != nil {
		if exec.IsCmdNotFound(err) {
			t.Skip(err)
		}

		t.Fatalf("unexpected error %v", err)
	}

	if err := scanner.Scan(); err != io.EOF {
		t.Errorf("expected io.EOF, got %v", err)
	}

	if scanner.offset != BlockSize {
		t.Errorf("expected %d bytes, got %d", BlockSize, scanner.offset)
	}

	return scanner.operations, payload.Bytes()
}

func TestFullUpdateScanOnes(t *testing.T) {
	ops, payload := checkFullScan(t, testOnes)

	checkReplaceBZ(t, ops, testOnes, testOnesHash, payload)
}

func TestFullUpdateScanRand(t *testing.T) {
	ops, payload := checkFullScan(t, testRand)

	checkReplace(t, ops, testRand, testRandHash, payload)
}

func TestFullUpdateScanUnaligned(t *testing.T) {
	scanner := fullScanner{
		payload: &bytes.Buffer{},
		source:  bytes.NewReader(testUnaligned),
	}

	if err := scanner.Scan(); err != errShortRead {
		t.Fatalf("expected errShortRead, got %v", err)
	}
}

func checkFullProc(t *testing.T, source, sourceHash []byte) *Procedure {
	f, err := ioutil.TempFile("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	defer os.Remove(f.Name())

	if _, err := f.Write(source); err != nil {
		t.Fatal(err)
	}

	proc, err := FullUpdate(f.Name())
	if system.IsOpNotSupported(err) {
		t.Skip("O_TMPFILE not supported")
	} else if exec.IsCmdNotFound(err) {
		t.Skip(err)
	} else if err != nil {
		t.Fatal(err)
	}

	if proc.NewInfo.GetSize() != BlockSize {
		t.Errorf("expected %d bytes, got %d", BlockSize, proc.NewInfo.GetSize())
	}

	if !bytes.Equal(proc.NewInfo.Hash, sourceHash) {
		t.Error("unexpected source hash")
	}

	return proc
}

func TestFullUpdateOnes(t *testing.T) {
	proc := checkFullProc(t, testOnes, testOnesHash)
	defer proc.Close()

	payload, err := ioutil.ReadAll(proc)
	if err != nil {
		t.Fatal(err)
	}

	checkReplaceBZ(t, proc.Operations, testOnes, testOnesHash, payload)
}

func TestFullUpdateRand(t *testing.T) {
	proc := checkFullProc(t, testRand, testRandHash)
	defer proc.Close()

	payload, err := ioutil.ReadAll(proc)
	if err != nil {
		t.Fatal(err)
	}

	checkReplace(t, proc.Operations, testRand, testRandHash, payload)
}
