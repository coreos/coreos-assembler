/*
 * MinIO Cloud Storage, (C) 2016 MinIO, Inc.
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

package cmd

import (
	"context"
	"io"
)

// StorageAPI interface.
type StorageAPI interface {
	// Stringified version of disk.
	String() string

	// Storage operations.
	IsOnline() bool // Returns true if disk is online.
	IsLocal() bool

	Hostname() string   // Returns host name if remote host.
	Endpoint() Endpoint // Returns endpoint.

	Close() error
	GetDiskID() (string, error)
	SetDiskID(id string)
	Healing() bool // Returns if disk is healing.

	DiskInfo(ctx context.Context) (info DiskInfo, err error)
	CrawlAndGetDataUsage(ctx context.Context, cache dataUsageCache) (dataUsageCache, error)

	// Volume operations.
	MakeVol(ctx context.Context, volume string) (err error)
	MakeVolBulk(ctx context.Context, volumes ...string) (err error)
	ListVols(ctx context.Context) (vols []VolInfo, err error)
	StatVol(ctx context.Context, volume string) (vol VolInfo, err error)
	DeleteVol(ctx context.Context, volume string, forceDelete bool) (err error)

	// WalkDir will walk a directory on disk and return a metacache stream on wr.
	WalkDir(ctx context.Context, opts WalkDirOptions, wr io.Writer) error

	// WalkVersions in sorted order directly on disk.
	WalkVersions(ctx context.Context, volume, dirPath, marker string, recursive bool, endWalkCh <-chan struct{}) (chan FileInfoVersions, error)
	// Walk in sorted order directly on disk.
	Walk(ctx context.Context, volume, dirPath, marker string, recursive bool, endWalkCh <-chan struct{}) (chan FileInfo, error)
	// Walk in sorted order directly on disk.
	WalkSplunk(ctx context.Context, volume, dirPath, marker string, endWalkCh <-chan struct{}) (chan FileInfo, error)

	// Metadata operations
	DeleteVersion(ctx context.Context, volume, path string, fi FileInfo) error
	DeleteVersions(ctx context.Context, volume string, versions []FileInfo) []error
	WriteMetadata(ctx context.Context, volume, path string, fi FileInfo) error
	ReadVersion(ctx context.Context, volume, path, versionID string, checkDataDir bool) (FileInfo, error)
	RenameData(ctx context.Context, srcVolume, srcPath, dataDir, dstVolume, dstPath string) error

	// File operations.
	ListDir(ctx context.Context, volume, dirPath string, count int) ([]string, error)
	ReadFile(ctx context.Context, volume string, path string, offset int64, buf []byte, verifier *BitrotVerifier) (n int64, err error)
	AppendFile(ctx context.Context, volume string, path string, buf []byte) (err error)
	CreateFile(ctx context.Context, volume, path string, size int64, reader io.Reader) error
	ReadFileStream(ctx context.Context, volume, path string, offset, length int64) (io.ReadCloser, error)
	RenameFile(ctx context.Context, srcVolume, srcPath, dstVolume, dstPath string) error
	CheckParts(ctx context.Context, volume string, path string, fi FileInfo) error
	CheckFile(ctx context.Context, volume string, path string) (err error)
	Delete(ctx context.Context, volume string, path string, recursive bool) (err error)
	VerifyFile(ctx context.Context, volume, path string, fi FileInfo) error

	// Write all data, syncs the data to disk.
	WriteAll(ctx context.Context, volume string, path string, reader io.Reader) (err error)

	// Read all.
	ReadAll(ctx context.Context, volume string, path string) (buf []byte, err error)
}

// storageReader is an io.Reader view of a disk
type storageReader struct {
	storage      StorageAPI
	volume, path string
	offset       int64
}

func (r *storageReader) Read(p []byte) (n int, err error) {
	nn, err := r.storage.ReadFile(context.TODO(), r.volume, r.path, r.offset, p, nil)
	r.offset += nn
	n = int(nn)

	if err == io.ErrUnexpectedEOF && nn > 0 {
		err = io.EOF
	}
	return
}

// storageWriter is a io.Writer view of a disk.
type storageWriter struct {
	storage      StorageAPI
	volume, path string
}

func (w *storageWriter) Write(p []byte) (n int, err error) {
	err = w.storage.AppendFile(context.TODO(), w.volume, w.path, p)
	if err == nil {
		n = len(p)
	}
	return
}

// StorageWriter returns a new io.Writer which appends data to the file
// at the given disk, volume and path.
func StorageWriter(storage StorageAPI, volume, path string) io.Writer {
	return &storageWriter{storage, volume, path}
}

// StorageReader returns a new io.Reader which reads data to the file
// at the given disk, volume, path and offset.
func StorageReader(storage StorageAPI, volume, path string, offset int64) io.Reader {
	return &storageReader{storage, volume, path, offset}
}
