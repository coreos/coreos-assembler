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
	"compress/bzip2"
	"crypto/sha256"
	"fmt"
	"hash"
	"io"
	"io/ioutil"
	"os"

	"github.com/coreos/mantle/update/metadata"
)

type Operation struct {
	hash.Hash
	io.LimitedReader

	Payload   *Payload
	Procedure *metadata.InstallProcedure
	Operation *metadata.InstallOperation
}

func NewOperation(payload *Payload, proc *metadata.InstallProcedure, op *metadata.InstallOperation) *Operation {
	sha := sha256.New()
	return &Operation{
		Hash: sha,
		LimitedReader: io.LimitedReader{
			R: io.TeeReader(payload, sha),
			N: int64(op.GetDataLength()),
		},
		Payload:   payload,
		Procedure: proc,
		Operation: op,
	}
}

func (op *Operation) Verify() error {
	switch op.Operation.GetType() {
	case metadata.InstallOperation_REPLACE:
		fallthrough
	case metadata.InstallOperation_REPLACE_BZ:
		if err := op.verifyOffset(); err != nil {
			return err
		}
		if len(op.Operation.SrcExtents) != 0 {
			return fmt.Errorf("replace contains source extents")
		}
		if _, err := io.Copy(ioutil.Discard, op); err != nil {
			return err
		}
		if err := op.verifyHash(); err != nil {
			return err
		}
	case metadata.InstallOperation_MOVE:
		return fmt.Errorf("MOVE")
	case metadata.InstallOperation_BSDIFF:
		return fmt.Errorf("BSDIFF")
	}

	return nil
}

func (op *Operation) verifyOffset() error {
	if int64(op.Operation.GetDataOffset()) != op.Payload.Offset {
		return fmt.Errorf("expected payload data offset %d not %d",
			op.Operation.DataOffset, op.Payload.Offset)
	}
	return nil
}

func (op *Operation) verifyHash() error {
	if len(op.Operation.DataSha256Hash) == 0 {
		return fmt.Errorf("missing payload data hash")
	}

	sum := op.Sum(nil)
	if !bytes.Equal(op.Operation.DataSha256Hash, sum) {
		return fmt.Errorf("expected payload data hash %x not %x",
			op.Operation.DataSha256Hash, sum)
	}

	return nil
}

func (op *Operation) Apply(dst, src *os.File) error {
	switch op.Operation.GetType() {
	case metadata.InstallOperation_REPLACE:
		return op.replace(dst, op)
	case metadata.InstallOperation_REPLACE_BZ:
		return op.replace(dst, bzip2.NewReader(op))
	case metadata.InstallOperation_MOVE:
		return op.move(dst, src)
	case metadata.InstallOperation_BSDIFF:
		return op.bsdiff(dst, src)
	}
	return fmt.Errorf("unknown operation type %s", op.Operation.GetType())
}

func (op *Operation) replace(dst *os.File, src io.Reader) error {
	if err := op.verifyOffset(); err != nil {
		return err
	}
	if len(op.Operation.SrcExtents) != 0 {
		return fmt.Errorf("replace contains source extents")
	}

	bs := int64(op.Payload.Manifest.GetBlockSize())
	maxSize := int64(op.Procedure.NewInfo.GetSize())
	for _, extent := range op.Operation.DstExtents {
		offset := int64(extent.GetStartBlock()) * bs
		length := int64(extent.GetNumBlocks()) * bs

		// BUG: update_engine only writes as much of an extent as it
		// has data for, allowing extents to be defined larger than
		// they actually should be. So far this only appears to happen
		// to the very last destination extent in a full update.
		if offset+length > maxSize {
			excess := (offset + length) - maxSize
			plog.Warningf("extent excedes destination bounds by %d bytes!", excess)
			length -= excess
		}

		if _, err := dst.Seek(offset, os.SEEK_SET); err != nil {
			return err
		}
		if _, err := io.CopyN(dst, src, length); err != nil {
			return err
		}
	}

	// BUG: On rare occasions (once so far) 4 bytes will get left behind in
	// a bzip2 compressed stream. Unclear what the bytes are and if they
	// are there due to a difference in implementation of bzip2 itself or
	// simply due to the algorithm being driven by reads instead of writes.
	if op.N != 0 {
		if op.Operation.GetType() == metadata.InstallOperation_REPLACE_BZ {
			plog.Warningf("Go's bzip2 left %d bytes unread!", op.N)
			if _, err := io.Copy(ioutil.Discard, op); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("replace left %d trailing bytes", op.N)
		}
	}

	return op.verifyHash()
}

func (op *Operation) move(dst, src *os.File) error {
	return fmt.Errorf("MOVE")
}

func (op *Operation) bsdiff(dst, src *os.File) error {
	return fmt.Errorf("BSDIFF")
}
