/*
 * zipindex, (C)2021 MinIO, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// Contains code that is
//
// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package zipindex

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/ioutil"
	"os"
	"time"
	"unicode/utf8"
)

var (
	// ErrFormat is returned when zip file cannot be parsed.
	ErrFormat = errors.New("zip: not a valid zip file")
	// ErrAlgorithm is returned if an unsupported compression type is used.
	ErrAlgorithm = errors.New("zip: unsupported compression algorithm")
	// ErrChecksum is returned if a file fails a CRC check.
	ErrChecksum = errors.New("zip: checksum error")
)

// ErrNeedMoreData is returned by ReadDir when more data is required to read the directory.
// The exact number of bytes from the end of the file is provided.
// It is reasonable to reject numbers that are too large to not run out of memory.
type ErrNeedMoreData struct {
	FromEnd int64
}

// Error returns the error as string.
func (e ErrNeedMoreData) Error() string {
	return fmt.Sprintf("provide more data: provide at least %d bytes back from EOF", e.FromEnd)
}

// FileFilter allows transforming the incoming data.
// If the returned file is nil it will not be added.
// Custom fields can be added. Note the Custom field will usually be nil.
type FileFilter = func(dst *File, entry *ZipDirEntry) *File

// DefaultFileFilter will filter out all entries that are not regular files and can be compressed.
func DefaultFileFilter(dst *File, entry *ZipDirEntry) *File {
	if !entry.Mode().IsRegular() {
		return nil
	}
	if decompressor(entry.Method) == nil {
		return nil
	}
	return dst
}

// ReadDir will read the directory from the provided buffer.
// Regular files that are expected to be decompressable will be returned.
// ErrNeedMoreData may be returned if more data is required to read the directory.
// For initial scan at least 64KiB or the entire file if smaller should be given,
// but more will make it more likely that the entire directory can be read.
// The total size of the zip file must be provided.
// A custom filter can be provided. If nil DefaultFileFilter will be used.
func ReadDir(buf []byte, zipSize int64, filter FileFilter) (Files, error) {
	if len(buf) > int(zipSize) {
		return nil, errors.New("more bytes than total size provided")
	}
	end, err := readDirectoryEnd(buf, zipSize)
	if err != nil {
		return nil, err
	}
	if end.directoryRecords > uint64(zipSize)/fileHeaderLen {
		return nil, fmt.Errorf("archive/zip: TOC declares impossible %d files in %d byte zip", end.directoryRecords, zipSize)
	}

	if filter == nil {
		filter = DefaultFileFilter
	}

	files := make([]File, 0, end.directoryRecords)
	wantAtEnd := zipSize - int64(end.directoryOffset)
	if wantAtEnd > int64(len(buf)) {
		return nil, ErrNeedMoreData{FromEnd: wantAtEnd}
	}
	offset := int64(len(buf)) - wantAtEnd
	bufR := bytes.NewReader(buf[offset:])

	// The count of files inside a zip is truncated to fit in a uint16.
	// Gloss over this by reading headers until we encounter
	// a bad one, and then only report an ErrFormat or UnexpectedEOF if
	// the file count modulo 65536 is incorrect.
	var entries int
	var f ZipDirEntry
	var file File
	for {
		f = ZipDirEntry{}
		err = readDirectoryHeader(&f, bufR)
		if err == ErrFormat || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			return nil, err
		}
		entries++

		file = File{
			Name:               f.Name,
			CompressedSize64:   f.CompressedSize64,
			UncompressedSize64: f.UncompressedSize64,
			Offset:             f.headerOffset,
			CRC32:              f.CRC32,
			Method:             f.Method,
			Flags:              f.Flags,
		}
		if filter != nil {
			if f2 := filter(&file, &f); f2 != nil {
				files = append(files, *f2)
			}
			continue
		}
		files = append(files, file)
	}
	if uint16(entries) != uint16(end.directoryRecords) { // only compare 16 bits here
		// Return the readDirectoryHeader error if we read
		// the wrong number of directory entries.
		return nil, err
	}
	return files, nil
}

// ReaderAt will read the directory from a io.ReaderAt.
// The total zip file must be provided.
// If the ZIP file directory exceeds maxDir bytes it will be rejected.
func ReaderAt(r io.ReaderAt, size, maxDir int64, filter FileFilter) (Files, error) {
	// Read 64K by default..
	sz := int64(64 << 10)
	var files Files
	for {
		if sz > size {
			sz = size
		}
		var buf = make([]byte, sz)
		_, err := r.ReadAt(buf, size-sz)
		if err != nil {
			return nil, err
		}
		files, err = ReadDir(buf, size, filter)
		if err == nil {
			return files, nil
		}
		var terr ErrNeedMoreData
		if errors.As(err, &terr) {
			if maxDir > 0 && terr.FromEnd > maxDir {
				return nil, errors.New("directory appears to exceed limit")
			}
			sz = terr.FromEnd
			continue
		}
		return nil, err
	}
}

// ReadFile will read the directory from a file.
// If the ZIP file directory exceeds 100MB it will be rejected.
func ReadFile(name string, filter FileFilter) (Files, error) {
	// Read 64K by default..
	sz := int64(64 << 10)
	var files Files
	for {
		f, err := os.Open(name)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		fi, err := f.Stat()
		if err != nil {
			return nil, err
		}
		if sz > fi.Size() {
			sz = fi.Size()
		}
		_, err = f.Seek(fi.Size()-sz, io.SeekStart)
		if err != nil {
			return nil, err
		}
		b, err := ioutil.ReadAll(f)
		if err != nil {
			return nil, err
		}
		files, err = ReadDir(b, fi.Size(), filter)
		if err == nil {
			return files, nil
		}
		var terr ErrNeedMoreData
		if errors.As(err, &terr) {
			if terr.FromEnd > 100<<20 {
				return nil, errors.New("directory appears to exceed 100MB")
			}
			sz = terr.FromEnd
			f.Close()
			continue
		}
		return nil, err
	}
}

type checksumReader struct {
	rc         io.ReadCloser
	compReader io.Reader // Reader that will only return compressed data.
	raw        io.Reader // Reader provided as input.
	hash       hash.Hash32
	nread      uint64 // number of bytes read so far
	f          *File
	err        error // sticky error
}

func (r *checksumReader) Read(b []byte) (n int, err error) {
	if r.err != nil {
		return 0, r.err
	}
	n, err = r.rc.Read(b)
	r.hash.Write(b[:n])
	r.nread += uint64(n)
	if err == nil {
		return
	}
	if err == io.EOF {
		if r.nread != r.f.UncompressedSize64 {
			return 0, io.ErrUnexpectedEOF
		}

		if r.f.hasDataDescriptor() {
			// If any compressed data remains, read it.
			io.Copy(ioutil.Discard, r.compReader)

			if err1 := readDataDescriptor(r.raw, r.f); err1 != nil {
				if err1 == io.EOF {
					err = io.ErrUnexpectedEOF
				} else {
					err = err1
				}
			} else if r.hash.Sum32() != r.f.CRC32 {
				err = ErrChecksum
			}
		}

		// If there's not a data descriptor, we still compare
		// the CRC32 of what we've read against the file header
		// or TOC's CRC32, if it seems like it was set.
		if r.f.CRC32 != 0 && r.hash.Sum32() != r.f.CRC32 {
			err = ErrChecksum
		}
	}
	r.err = err
	return
}

func (r *checksumReader) Close() error { return r.rc.Close() }

func (f *File) hasDataDescriptor() bool {
	return f.Flags&0x8 != 0
}

// findBodyOffset does the minimum work to verify the file has a header
// and returns the file body offset.
func (f *File) skipToBody(r io.Reader) error {
	var buf [fileHeaderLen]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return err
	}
	b := readBuf(buf[:])
	if sig := b.uint32(); sig != fileHeaderSignature {
		return ErrFormat
	}
	b = b[22:] // skip over most of the header
	filenameLen := int(b.uint16())
	extraLen := int(b.uint16())
	// Skip extra...
	_, err := io.CopyN(ioutil.Discard, r, int64(filenameLen+extraLen))
	return err
}

func readDataDescriptor(r io.Reader, f *File) error {
	var buf [dataDescriptorLen]byte

	// The spec says: "Although not originally assigned a
	// signature, the value 0x08074b50 has commonly been adopted
	// as a signature value for the data descriptor record.
	// Implementers should be aware that ZIP files may be
	// encountered with or without this signature marking data
	// descriptors and should account for either case when reading
	// ZIP files to ensure compatibility."
	//
	// dataDescriptorLen includes the size of the signature but
	// first read just those 4 bytes to see if it exists.
	if _, err := io.ReadFull(r, buf[:4]); err != nil {
		return err
	}
	off := 0
	maybeSig := readBuf(buf[:4])
	if maybeSig.uint32() != dataDescriptorSignature {
		// No data descriptor signature. Keep these four
		// bytes.
		off += 4
	}
	if _, err := io.ReadFull(r, buf[off:12]); err != nil {
		return err
	}
	b := readBuf(buf[:12])
	if b.uint32() != f.CRC32 {
		return ErrChecksum
	}

	// The two sizes that follow here can be either 32 bits or 64 bits
	// but the spec is not very clear on this and different
	// interpretations has been made causing incompatibilities. We
	// already have the sizes from the central directory so we can
	// just ignore these.

	return nil
}

// readDirectoryHeader attempts to read a directory header from r.
// It returns io.ErrUnexpectedEOF if it cannot read a complete header,
// and ErrFormat if it doesn't find a valid header signature.
func readDirectoryHeader(f *ZipDirEntry, r io.Reader) error {
	var buf [directoryHeaderLen]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return err
	}
	b := readBuf(buf[:])
	if sig := b.uint32(); sig != directoryHeaderSignature {
		return ErrFormat
	}
	f.CreatorVersion = b.uint16()
	f.ReaderVersion = b.uint16()
	f.Flags = b.uint16()
	f.Method = b.uint16()
	f.modifiedTime = b.uint16()
	f.modifiedDate = b.uint16()
	f.CRC32 = b.uint32()
	f.compressedSize = b.uint32()
	f.uncompressedSize = b.uint32()
	f.CompressedSize64 = uint64(f.compressedSize)
	f.UncompressedSize64 = uint64(f.uncompressedSize)
	filenameLen := int(b.uint16())
	extraLen := int(b.uint16())
	commentLen := int(b.uint16())
	b = b[4:] // skipped start disk number and internal attributes (2x uint16)
	f.ExternalAttrs = b.uint32()
	f.headerOffset = int64(b.uint32())
	d := make([]byte, filenameLen+extraLen+commentLen)
	if _, err := io.ReadFull(r, d); err != nil {
		return err
	}
	f.Name = string(d[:filenameLen])
	f.Extra = d[filenameLen : filenameLen+extraLen]
	f.Comment = string(d[filenameLen+extraLen:])

	// Determine the character encoding.
	utf8Valid1, utf8Require1 := detectUTF8(f.Name)
	utf8Valid2, utf8Require2 := detectUTF8(f.Comment)
	switch {
	case !utf8Valid1 || !utf8Valid2:
		// Name and Comment definitely not UTF-8.
		f.NonUTF8 = true
	case !utf8Require1 && !utf8Require2:
		// Name and Comment use only single-byte runes that overlap with UTF-8.
		f.NonUTF8 = false
	default:
		// Might be UTF-8, might be some other encoding; preserve existing flag.
		// Some ZIP writers use UTF-8 encoding without setting the UTF-8 flag.
		// Since it is impossible to always distinguish valid UTF-8 from some
		// other encoding (e.g., GBK or Shift-JIS), we trust the flag.
		f.NonUTF8 = f.Flags&0x800 == 0
	}

	needUSize := f.uncompressedSize == ^uint32(0)
	needCSize := f.compressedSize == ^uint32(0)
	needHeaderOffset := f.headerOffset == int64(^uint32(0))

	// Best effort to find what we need.
	// Other zip authors might not even follow the basic format,
	// and we'll just ignore the Extra content in that case.
	var modified time.Time
parseExtras:
	for extra := readBuf(f.Extra); len(extra) >= 4; { // need at least tag and size
		fieldTag := extra.uint16()
		fieldSize := int(extra.uint16())
		if len(extra) < fieldSize {
			break
		}
		fieldBuf := extra.sub(fieldSize)

		switch fieldTag {
		case zip64ExtraID:
			// update directory values from the zip64 extra block.
			// They should only be consulted if the sizes read earlier
			// are maxed out.
			// See golang.org/issue/13367.
			if needUSize {
				needUSize = false
				if len(fieldBuf) < 8 {
					return ErrFormat
				}
				f.UncompressedSize64 = fieldBuf.uint64()
			}
			if needCSize {
				needCSize = false
				if len(fieldBuf) < 8 {
					return ErrFormat
				}
				f.CompressedSize64 = fieldBuf.uint64()
			}
			if needHeaderOffset {
				needHeaderOffset = false
				if len(fieldBuf) < 8 {
					return ErrFormat
				}
				f.headerOffset = int64(fieldBuf.uint64())
			}
		case ntfsExtraID:
			if len(fieldBuf) < 4 {
				continue parseExtras
			}
			fieldBuf.uint32()        // reserved (ignored)
			for len(fieldBuf) >= 4 { // need at least tag and size
				attrTag := fieldBuf.uint16()
				attrSize := int(fieldBuf.uint16())
				if len(fieldBuf) < attrSize {
					continue parseExtras
				}
				attrBuf := fieldBuf.sub(attrSize)
				if attrTag != 1 || attrSize != 24 {
					continue // Ignore irrelevant attributes
				}

				const ticksPerSecond = 1e7    // Windows timestamp resolution
				ts := int64(attrBuf.uint64()) // ModTime since Windows epoch
				secs := ts / ticksPerSecond
				nsecs := (1e9 / ticksPerSecond) * (ts % ticksPerSecond)
				epoch := time.Date(1601, time.January, 1, 0, 0, 0, 0, time.UTC)
				modified = time.Unix(epoch.Unix()+secs, nsecs)
			}
		case unixExtraID, infoZipUnixExtraID:
			if len(fieldBuf) < 8 {
				continue parseExtras
			}
			fieldBuf.uint32()              // AcTime (ignored)
			ts := int64(fieldBuf.uint32()) // ModTime since Unix epoch
			modified = time.Unix(ts, 0)
		case extTimeExtraID:
			if len(fieldBuf) < 5 || fieldBuf.uint8()&1 == 0 {
				continue parseExtras
			}
			ts := int64(fieldBuf.uint32()) // ModTime since Unix epoch
			modified = time.Unix(ts, 0)
		}
	}

	msdosModified := msDosTimeToTime(f.modifiedDate, f.modifiedTime)
	f.Modified = msdosModified
	if !modified.IsZero() {
		f.Modified = modified.UTC()

		// If legacy MS-DOS timestamps are set, we can use the delta between
		// the legacy and extended versions to estimate timezone offset.
		//
		// A non-UTC timezone is always used (even if offset is zero).
		// Thus, ZipDirEntry.Modified.Location() == time.UTC is useful for
		// determining whether extended timestamps are present.
		// This is necessary for users that need to do additional time
		// calculations when dealing with legacy ZIP formats.
		if f.modifiedTime != 0 || f.modifiedDate != 0 {
			f.Modified = modified.In(timeZone(msdosModified.Sub(modified)))
		}
	}

	// Assume that uncompressed size 2³²-1 could plausibly happen in
	// an old zip32 file that was sharding inputs into the largest chunks
	// possible (or is just malicious; search the web for 42.zip).
	// If needUSize is true still, it means we didn't see a zip64 extension.
	// As long as the compressed size is not also 2³²-1 (implausible)
	// and the header is not also 2³²-1 (equally implausible),
	// accept the uncompressed size 2³²-1 as valid.
	// If nothing else, this keeps archive/zip working with 42.zip.
	_ = needUSize

	if needCSize || needHeaderOffset {
		return ErrFormat
	}

	return nil
}

func readDirectoryEnd(buf []byte, size int64) (dir *directoryEnd, err error) {
	// look for directoryEndSignature in the last 1k, then in the last 65k
	offset := findSignatureInBlock(buf)
	if offset < 0 {
		// No signature
		return nil, ErrFormat
	}

	// read header into struct
	b := readBuf(buf[offset+4:]) // skip signature
	d := &directoryEnd{
		diskNbr:            uint32(b.uint16()),
		dirDiskNbr:         uint32(b.uint16()),
		dirRecordsThisDisk: uint64(b.uint16()),
		directoryRecords:   uint64(b.uint16()),
		directorySize:      uint64(b.uint32()),
		directoryOffset:    uint64(b.uint32()),
		commentLen:         b.uint16(),
	}
	l := int(d.commentLen)
	if l > len(b) {
		return nil, errors.New("zip: invalid comment length")
	}
	d.comment = string(b[:l])

	// These values mean that the file can be a zip64 file
	if d.directoryRecords == 0xffff || d.directorySize == 0xffff || d.directoryOffset == 0xffffffff {
		p, err := findDirectory64End(buf, offset)
		fromEnd := size - p
		if err == nil && p >= 0 {
			if fromEnd > int64(len(buf)) {
				return nil, ErrNeedMoreData{FromEnd: fromEnd}
			}
			err = readDirectory64End(bytes.NewReader(buf), int64(len(buf))-fromEnd, d)
		}
		if err != nil {
			return nil, err
		}
	}
	// Make sure directoryOffset points to somewhere in our file.
	if o := int64(d.directoryOffset); o < 0 || o >= size {
		return nil, ErrFormat
	}
	return d, nil
}

// findDirectory64End tries to read the zip64 locator just before the
// directory end and returns the offset of the zip64 directory end if
// found.
func findDirectory64End(buf []byte, directoryEndOffset int) (int64, error) {
	locOffset := directoryEndOffset - directory64LocLen
	if locOffset < 0 {
		return -1, nil // no need to look for a header outside the file
	}
	b := readBuf(buf[locOffset : locOffset+directory64LocLen])
	if sig := b.uint32(); sig != directory64LocSignature {
		return -1, nil
	}
	if b.uint32() != 0 { // number of the disk with the start of the zip64 end of central directory
		return -1, nil // the file is not a valid zip64-file
	}
	p := b.uint64()      // relative offset of the zip64 end of central directory record
	if b.uint32() != 1 { // total number of disks
		return -1, nil // the file is not a valid zip64-file
	}
	return int64(p), nil
}

// readDirectory64End reads the zip64 directory end and updates the
// directory end with the zip64 directory end values.
func readDirectory64End(r io.ReaderAt, offset int64, d *directoryEnd) (err error) {
	buf := make([]byte, directory64EndLen)
	if _, err := r.ReadAt(buf, offset); err != nil {
		return err
	}

	b := readBuf(buf)
	if sig := b.uint32(); sig != directory64EndSignature {
		return ErrFormat
	}

	b = b[12:]                        // skip dir size, version and version needed (uint64 + 2x uint16)
	d.diskNbr = b.uint32()            // number of this disk
	d.dirDiskNbr = b.uint32()         // number of the disk with the start of the central directory
	d.dirRecordsThisDisk = b.uint64() // total number of entries in the central directory on this disk
	d.directoryRecords = b.uint64()   // total number of entries in the central directory
	d.directorySize = b.uint64()      // size of the central directory
	d.directoryOffset = b.uint64()    // offset of start of central directory with respect to the starting disk number

	return nil
}

func findSignatureInBlock(b []byte) int {
	for i := len(b) - directoryEndLen; i >= 0; i-- {
		// defined from directoryEndSignature in struct.go
		if b[i] == 'P' && b[i+1] == 'K' && b[i+2] == 0x05 && b[i+3] == 0x06 {
			// n is length of comment
			n := int(b[i+directoryEndLen-2]) | int(b[i+directoryEndLen-1])<<8
			if n+directoryEndLen+i <= len(b) {
				return i
			}
		}
	}
	return -1
}

type readBuf []byte

func (b *readBuf) uint8() uint8 {
	v := (*b)[0]
	*b = (*b)[1:]
	return v
}

func (b *readBuf) uint16() uint16 {
	v := binary.LittleEndian.Uint16(*b)
	*b = (*b)[2:]
	return v
}

func (b *readBuf) uint32() uint32 {
	v := binary.LittleEndian.Uint32(*b)
	*b = (*b)[4:]
	return v
}

func (b *readBuf) uint64() uint64 {
	v := binary.LittleEndian.Uint64(*b)
	*b = (*b)[8:]
	return v
}

func (b *readBuf) sub(n int) readBuf {
	b2 := (*b)[:n]
	*b = (*b)[n:]
	return b2
}

// detectUTF8 reports whether s is a valid UTF-8 string, and whether the string
// must be considered UTF-8 encoding (i.e., not compatible with CP-437, ASCII,
// or any other common encoding).
func detectUTF8(s string) (valid, require bool) {
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		// Officially, ZIP uses CP-437, but many readers use the system's
		// local character encoding. Most encoding are compatible with a large
		// subset of CP-437, which itself is ASCII-like.
		//
		// Forbid 0x7e and 0x5c since EUC-KR and Shift-JIS replace those
		// characters with localized currency and overline characters.
		if r < 0x20 || r > 0x7d || r == 0x5c {
			if !utf8.ValidRune(r) || (r == utf8.RuneError && size == 1) {
				return false, false
			}
			require = true
		}
	}
	return true, require
}
