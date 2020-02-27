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
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/golang/protobuf/proto"

	"github.com/coreos/mantle/system"
	"github.com/coreos/mantle/update/metadata"
)

var (
	errShortRead = errors.New("read an incomplete block")
)

// FullUpdate generates an update Procedure for the given file, embedding its
// entire contents in the payload so it does not depend any previous state.
func FullUpdate(path string) (*Procedure, error) {
	source, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer source.Close()

	info, err := NewInstallInfo(source)
	if err != nil {
		return nil, err
	}

	payload, err := system.PrivateFile("")
	if err != nil {
		return nil, err
	}

	scanner := fullScanner{payload: payload, source: source}
	for err == nil {
		err = scanner.Scan()
	}
	if err != nil && err != io.EOF {
		payload.Close()
		if err == errShortRead {
			err = fmt.Errorf("%s: %v", path, err)
		}
		return nil, err
	}

	if _, err := payload.Seek(0, os.SEEK_SET); err != nil {
		payload.Close()
		return nil, err
	}

	return &Procedure{
		InstallProcedure: metadata.InstallProcedure{
			NewInfo:    info,
			Operations: scanner.operations,
		},
		ReadCloser: payload,
	}, nil
}

type fullScanner struct {
	payload    io.Writer
	source     io.Reader
	offset     uint64
	operations []*metadata.InstallOperation
}

func (f *fullScanner) readChunk() ([]byte, error) {
	chunk := make([]byte, ChunkSize)
	n, err := io.ReadFull(f.source, chunk)
	if (err == io.EOF || err == io.ErrUnexpectedEOF) && n != 0 {
		err = nil
	}
	return chunk[:n], err
}

func (f *fullScanner) Scan() error {
	chunk, err := f.readChunk()
	if err != nil {
		return err
	}
	if len(chunk)%BlockSize != 0 {
		return errShortRead
	}

	startBlock := uint64(f.offset) / BlockSize
	numBlocks := uint64(len(chunk)) / BlockSize
	f.offset += uint64(len(chunk))

	// Try bzip2 compressing the data, hopefully it will shrink!
	opType := metadata.InstallOperation_REPLACE_BZ
	opData, err := Bzip2(chunk)
	if err != nil {
		return err
	}

	if len(opData) >= len(chunk) {
		// That was disappointing, use the uncompressed data instead.
		opType = metadata.InstallOperation_REPLACE
		opData = chunk
	}

	if _, err := f.payload.Write(opData); err != nil {
		return err
	}

	// Operation.DataOffset is filled in by Generator.updateOffsets
	sum := sha256.Sum256(opData)
	op := &metadata.InstallOperation{
		Type: opType.Enum(),
		DstExtents: []*metadata.Extent{&metadata.Extent{
			StartBlock: proto.Uint64(startBlock),
			NumBlocks:  proto.Uint64(numBlocks),
		}},
		DataLength:     proto.Uint32(uint32(len(opData))),
		DataSha256Hash: sum[:],
	}

	f.operations = append(f.operations, op)

	return nil
}
