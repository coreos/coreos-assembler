/*
 * MinIO Cloud Storage, (C) 2020 MinIO, Inc.
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
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/gorilla/mux"
	"github.com/minio/minio/cmd/logger"
)

// WalkDirOptions provides options for WalkDir operations.
type WalkDirOptions struct {
	// Bucket to crawl
	Bucket string

	// Directory inside the bucket.
	BaseDir string

	// Do a full recursive scan.
	Recursive bool
}

// WalkDir will traverse a directory and return all entries found.
// On success a sorted meta cache stream will be returned.
func (s *xlStorage) WalkDir(ctx context.Context, opts WalkDirOptions, wr io.Writer) error {
	atomic.AddInt32(&s.activeIOCount, 1)
	defer func() {
		atomic.AddInt32(&s.activeIOCount, -1)
	}()

	// Verify if volume is valid and it exists.
	volumeDir, err := s.getVolDir(opts.Bucket)
	if err != nil {
		return err
	}

	// Stat a volume entry.
	_, err = os.Stat(volumeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return errVolumeNotFound
		} else if isSysErrIO(err) {
			return errFaultyDisk
		}
		return err
	}

	// Fast exit track to check if we are listing an object with
	// a trailing slash, this will avoid to list the object content.
	if HasSuffix(opts.BaseDir, SlashSeparator) {
		if st, err := os.Stat(pathJoin(volumeDir, opts.BaseDir, xlStorageFormatFile)); err == nil && st.Mode().IsRegular() {
			return errFileNotFound
		}
	}
	// Use a small block size to start sending quickly
	w := newMetacacheWriter(wr, 16<<10)
	defer w.Close()
	out, err := w.stream()
	if err != nil {
		return err
	}
	defer close(out)

	var scanDir func(path string) error
	scanDir = func(current string) error {
		entries, err := s.ListDir(ctx, opts.Bucket, current, -1)
		if err != nil {
			// Folder could have gone away in-between
			if err != errVolumeNotFound && err != errFileNotFound {
				logger.LogIf(ctx, err)
			}
			// Forward some errors?
			return nil
		}

		for i, entry := range entries {
			if strings.HasSuffix(entry, slashSeparator) {
				// Trim slash, maybe compiler is clever?
				entries[i] = entries[i][:len(entry)-1]
				continue
			}
			// Do do not retain the file.
			entries[i] = ""

			// If root was an object return it as such.
			if HasSuffix(entry, xlStorageFormatFile) {
				var meta metaCacheEntry
				meta.metadata, err = ioutil.ReadFile(pathJoin(volumeDir, meta.name, xlStorageFormatFile))
				if err != nil {
					logger.LogIf(ctx, err)
					continue
				}
				meta.name = strings.TrimSuffix(meta.name, xlStorageFormatFile)
				meta.name = strings.TrimSuffix(meta.name, SlashSeparator)
				out <- meta
				return nil
			}
			// Check legacy.
			if HasSuffix(entry, xlStorageFormatFileV1) {
				var meta metaCacheEntry
				meta.metadata, err = ioutil.ReadFile(pathJoin(volumeDir, meta.name, xlStorageFormatFileV1))
				if err != nil {
					logger.LogIf(ctx, err)
					continue
				}
				meta.name = strings.TrimSuffix(meta.name, xlStorageFormatFileV1)
				meta.name = strings.TrimSuffix(meta.name, SlashSeparator)
				out <- meta
				return nil
			}
			// Skip all other files.
		}

		// Process in sort order.
		sort.Strings(entries)
		dirStack := make([]string, 0, 5)
		for _, entry := range entries {
			if entry == "" {
				continue
			}
			meta := metaCacheEntry{name: PathJoin(current, entry)}

			// If directory entry on stack before this, pop it now.
			for len(dirStack) > 0 && dirStack[len(dirStack)-1] < meta.name {
				pop := dirStack[len(dirStack)-1]
				out <- metaCacheEntry{name: pop}
				if opts.Recursive {
					// Scan folder we found. Should be in correct sort order where we are.
					err := scanDir(pop)
					logger.LogIf(ctx, err)
				}
				dirStack = dirStack[:len(dirStack)-1]
			}

			// All objects will be returned as directories, there has been no object check yet.
			// Check it by attempting to read metadata.
			meta.metadata, err = ioutil.ReadFile(pathJoin(volumeDir, meta.name, xlStorageFormatFile))
			switch {
			case err == nil:
				// It was an object
				out <- meta
			case os.IsNotExist(err):
				meta.metadata, err = ioutil.ReadFile(pathJoin(volumeDir, meta.name, xlStorageFormatFileV1))
				if err == nil {
					// Maybe rename? Would make it inconsistent across disks though.
					// os.Rename(pathJoin(volumeDir, meta.name, xlStorageFormatFileV1), pathJoin(volumeDir, meta.name, xlStorageFormatFile))
					// It was an object
					out <- meta
					continue
				}

				// NOT an object, append to stack (with slash)
				dirStack = append(dirStack, meta.name+slashSeparator)
			default:
				logger.LogIf(ctx, err)
			}
		}
		// If directory entry left on stack, pop it now.
		for len(dirStack) > 0 {
			pop := dirStack[len(dirStack)-1]
			out <- metaCacheEntry{name: pop}
			if opts.Recursive {
				// Scan folder we found. Should be in correct sort order where we are.
				err := scanDir(pop)
				logger.LogIf(ctx, err)
			}
			dirStack = dirStack[:len(dirStack)-1]
		}
		return nil
	}

	// Stream output.
	return scanDir(opts.BaseDir)
}

func (p *xlStorageDiskIDCheck) WalkDir(ctx context.Context, opts WalkDirOptions, wr io.Writer) error {
	if err := p.checkDiskStale(); err != nil {
		return err
	}
	return p.storage.WalkDir(ctx, opts, wr)
}

// WalkDir will traverse a directory and return all entries found.
// On success a meta cache stream will be returned, that should be closed when done.
func (client *storageRESTClient) WalkDir(ctx context.Context, opts WalkDirOptions, wr io.Writer) error {
	values := make(url.Values)
	values.Set(storageRESTVolume, opts.Bucket)
	values.Set(storageRESTDirPath, opts.BaseDir)
	values.Set(storageRESTRecursive, strconv.FormatBool(opts.Recursive))
	respBody, err := client.call(ctx, storageRESTMethodWalkDir, values, nil, -1)
	if err != nil {
		logger.LogIf(ctx, err)
		return err
	}
	return waitForHTTPStream(respBody, wr)
}

// WalkDirHandler - remote caller to list files and folders in a requested directory path.
func (s *storageRESTServer) WalkDirHandler(w http.ResponseWriter, r *http.Request) {
	if !s.IsValid(w, r) {
		return
	}
	vars := mux.Vars(r)
	volume := vars[storageRESTVolume]
	dirPath := vars[storageRESTDirPath]
	recursive, err := strconv.ParseBool(vars[storageRESTRecursive])
	if err != nil {
		s.writeErrorResponse(w, err)
		return
	}
	writer := streamHTTPResponse(w)
	writer.CloseWithError(s.storage.WalkDir(r.Context(), WalkDirOptions{Bucket: volume, BaseDir: dirPath, Recursive: recursive}, writer))
}
