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

package update

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/ioutil"

	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/golang/protobuf/proto"

	"github.com/coreos/mantle/update/metadata"
)

var (
	InvalidMagic     = errors.New("payload missing magic prefix")
	InvalidVersion   = errors.New("payload version unsupported")
	InvalidBlockSize = errors.New("payload block size not 4096")
)

const (
	// internal-only procedure type for mapping the special partition
	// fields in DeltaArchiveManifest to the more generic data type.
	installProcedure_partition metadata.InstallProcedure_Type = -1
)

type Payload struct {
	h hash.Hash
	r io.Reader

	// Offset is the number of bytes read from the payload,
	// excluding the header and manifest.
	Offset int64

	// Parsed metadata contained in the payload.
	Header     metadata.DeltaArchiveHeader
	Manifest   metadata.DeltaArchiveManifest
	Signatures metadata.Signatures
}

func NewPayloadFrom(r io.Reader) (*Payload, error) {
	h := NewSignatureHash()
	p := &Payload{h: h, r: r}

	if err := p.readHeader(); err != nil {
		return nil, err
	}

	if err := p.readManifest(); err != nil {
		return nil, err
	}

	// Reset offset to 0, all offset values in the manifest are
	// relative to the end of the manifest within the payload.
	p.Offset = 0

	return p, nil
}

// Read reads from the raw payload stream, updating Hash and Offset for
// later verification. Behaves similarly to io.TeeReader.
func (p *Payload) Read(b []byte) (n int, err error) {
	n, err = p.r.Read(b)
	if n > 0 {
		p.Offset += int64(n)
		if n, err := p.h.Write(b[:n]); err != nil {
			return n, err
		}
	}
	return
}

// Sum returns the hash of the payload read so far.
func (p *Payload) Sum() []byte {
	return p.h.Sum(nil)
}

func (p *Payload) readHeader() error {
	if err := binary.Read(p, binary.BigEndian, &p.Header); err != nil {
		return err
	}

	if string(p.Header.Magic[:]) != metadata.Magic {
		return InvalidMagic
	}

	if p.Header.Version != metadata.Version {
		return InvalidVersion
	}

	return nil
}

func (p *Payload) readManifest() error {
	if p.Header.ManifestSize == 0 {
		return fmt.Errorf("missing manifest")
	}

	buf := make([]byte, p.Header.ManifestSize)
	if _, err := io.ReadFull(p, buf); err != nil {
		return err
	}

	if err := proto.Unmarshal(buf, &p.Manifest); err != nil {
		return err
	}

	if p.Manifest.GetBlockSize() != 4096 {
		return InvalidBlockSize
	}

	return nil
}

// VerifySignature reads and checks for a valid signature.
func (p *Payload) VerifySignature() error {
	if p.Manifest.GetSignaturesOffset() != uint64(p.Offset) {
		return fmt.Errorf("expected signature offset %d, not %d",
			p.Manifest.GetSignaturesOffset(), p.Offset)
	}

	// Get the final hash of the signed portion of the payload.
	sum := p.Sum()

	buf := make([]byte, p.Manifest.GetSignaturesSize())
	if _, err := io.ReadFull(p, buf); err != nil {
		return err
	}

	if err := proto.Unmarshal(buf, &p.Signatures); err != nil {
		return err
	}

	if err := VerifySignature(sum, &p.Signatures); err != nil {
		return err
	}

	// There shouldn't be any extra data following the signatures.
	if n, err := io.Copy(ioutil.Discard, p); err != nil {
		return fmt.Errorf("trailing read failure: %v", err)
	} else if n != 0 {
		return fmt.Errorf("found %d trailing bytes", n)
	}

	return nil
}

func (p *Payload) Procedures() []*metadata.InstallProcedure {
	procs := []*metadata.InstallProcedure{
		&metadata.InstallProcedure{
			Type:       installProcedure_partition.Enum(),
			Operations: p.Manifest.PartitionOperations,
			OldInfo:    p.Manifest.OldPartitionInfo,
			NewInfo:    p.Manifest.NewPartitionInfo,
		},
	}
	return append(procs, p.Manifest.Procedures...)
}

func (p *Payload) Operations(proc *metadata.InstallProcedure) []*metadata.InstallOperation {
	return proc.Operations
}

// Verify reads the entire payload and checks it for errors.
func (p *Payload) Verify() error {
	progress := 0
	for _, proc := range p.Procedures() {
		for _, op := range p.Operations(proc) {
			progress++
			if err := p.verifyOp(op); err != nil {
				return fmt.Errorf("operation %d: %v\n%s",
					progress, err,
					proto.MarshalTextString(op))
			}
		}
	}

	return p.VerifySignature()
}

func (p *Payload) verifyOp(op *metadata.InstallOperation) error {
	if int64(op.GetDataOffset()) != p.Offset {
		return fmt.Errorf("expected payload offset %d not %d",
			op.DataOffset, p.Offset)
	}

	if len(op.DataSha256Hash) == 0 {
		return fmt.Errorf("missing payload data hash")
	}

	hashOp := sha256.New()
	if _, err := io.CopyN(hashOp, p, int64(op.GetDataLength())); err != nil {
		return err
	}
	sum := hashOp.Sum(nil)

	if !bytes.Equal(op.DataSha256Hash, sum) {
		return fmt.Errorf("expected payload data hash %x not %x",
			op.DataSha256Hash, sum)
	}

	return nil
}
