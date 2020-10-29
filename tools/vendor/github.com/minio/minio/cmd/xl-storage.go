/*
 * MinIO Cloud Storage, (C) 2016-2020 MinIO, Inc.
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
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/url"
	"os"
	"path"
	slashpath "path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/dustin/go-humanize"
	jsoniter "github.com/json-iterator/go"
	"github.com/klauspost/readahead"
	"github.com/minio/minio/cmd/config"
	"github.com/minio/minio/cmd/logger"
	"github.com/minio/minio/pkg/disk"
	"github.com/minio/minio/pkg/env"
	xioutil "github.com/minio/minio/pkg/ioutil"
	"github.com/minio/minio/pkg/madmin"
)

const (
	nullVersionID     = "null"
	diskMinTotalSpace = 900 * humanize.MiByte // Min 900MiB total space.
	readBlockSize     = 4 * humanize.MiByte   // Default read block size 4MiB.

	// On regular files bigger than this;
	readAheadSize = 16 << 20
	// Read this many buffers ahead.
	readAheadBuffers = 4
	// Size of each buffer.
	readAheadBufSize = 1 << 20

	// Wait interval to check if active IO count is low
	// to proceed crawling to compute data usage.
	// Wait up to lowActiveIOWaitMaxN times.
	lowActiveIOWaitTick = 100 * time.Millisecond
	lowActiveIOWaitMaxN = 10

	// XL metadata file carries per object metadata.
	xlStorageFormatFile = "xl.meta"
)

// isValidVolname verifies a volname name in accordance with object
// layer requirements.
func isValidVolname(volname string) bool {
	if len(volname) < 3 {
		return false
	}

	if runtime.GOOS == "windows" {
		// Volname shouldn't have reserved characters in Windows.
		return !strings.ContainsAny(volname, `\:*?\"<>|`)
	}

	return true
}

// xlStorage - implements StorageAPI interface.
type xlStorage struct {
	maxActiveIOCount int32
	activeIOCount    int32

	diskPath string
	endpoint Endpoint

	pool sync.Pool

	globalSync bool

	rootDisk bool

	diskID string

	formatFileInfo  os.FileInfo
	formatLastCheck time.Time

	diskInfoCache timedValue

	ctx context.Context
	sync.RWMutex
}

// checkPathLength - returns error if given path name length more than 255
func checkPathLength(pathName string) error {
	// Apple OS X path length is limited to 1016
	if runtime.GOOS == "darwin" && len(pathName) > 1016 {
		return errFileNameTooLong
	}

	// Disallow more than 1024 characters on windows, there
	// are no known name_max limits on Windows.
	if runtime.GOOS == "windows" && len(pathName) > 1024 {
		return errFileNameTooLong
	}

	// On Unix we reject paths if they are just '.', '..' or '/'
	if pathName == "." || pathName == ".." || pathName == slashSeparator {
		return errFileAccessDenied
	}

	// Check each path segment length is > 255 on all Unix
	// platforms, look for this value as NAME_MAX in
	// /usr/include/linux/limits.h
	var count int64
	for _, p := range pathName {
		switch p {
		case '/':
			count = 0 // Reset
		case '\\':
			if runtime.GOOS == globalWindowsOSName {
				count = 0
			}
		default:
			count++
			if count > 255 {
				return errFileNameTooLong
			}
		}
	} // Success.
	return nil
}

func getValidPath(path string, requireDirectIO bool) (string, error) {
	if path == "" {
		return path, errInvalidArgument
	}

	var err error
	// Disallow relative paths, figure out absolute paths.
	path, err = filepath.Abs(path)
	if err != nil {
		return path, err
	}

	fi, err := os.Stat(path)
	if err != nil && !os.IsNotExist(err) {
		return path, err
	}
	if os.IsNotExist(err) {
		// Disk not found create it.
		if err = os.MkdirAll(path, 0777); err != nil {
			return path, err
		}
	}
	if fi != nil && !fi.IsDir() {
		return path, errDiskNotDir
	}

	// check if backend is writable.
	var rnd [8]byte
	_, _ = rand.Read(rnd[:])

	fn := pathJoin(path, ".writable-check-"+hex.EncodeToString(rnd[:])+".tmp")
	defer os.Remove(fn)

	var file *os.File

	if requireDirectIO {
		// only erasure coding needs direct-io support
		file, err = disk.OpenFileDirectIO(fn, os.O_CREATE|os.O_EXCL, 0666)
	} else {
		file, err = os.OpenFile(fn, os.O_CREATE|os.O_EXCL, 0666)
	}

	// open file in direct I/O and use default umask, this also verifies
	// if direct i/o failed.
	if err != nil {
		if isSysErrInvalidArg(err) {
			// O_DIRECT not supported
			return path, errUnsupportedDisk
		}
		return path, osErrToFileErr(err)
	}
	file.Close()

	di, err := getDiskInfo(path)
	if err != nil {
		return path, err
	}

	if err = checkDiskMinTotal(di); err != nil {
		return path, err
	}

	return path, nil
}

// isDirEmpty - returns whether given directory is empty or not.
func isDirEmpty(dirname string) bool {
	f, err := os.Open(dirname)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.LogIf(GlobalContext, err)
		}

		return false
	}
	defer f.Close()
	// List one entry.
	if _, err = f.Readdirnames(1); err != io.EOF {
		if !os.IsNotExist(err) {
			logger.LogIf(GlobalContext, err)
		}
		return false
	}
	// Returns true if we have reached EOF, directory is indeed empty.
	return true
}

// Initialize a new storage disk.
func newLocalXLStorage(path string) (*xlStorage, error) {
	u := url.URL{Path: path}
	return newXLStorage(Endpoint{
		URL:     &u,
		IsLocal: true,
	})
}

// Initialize a new storage disk.
func newXLStorage(ep Endpoint) (*xlStorage, error) {
	path := ep.Path
	var err error
	if path, err = getValidPath(path, true); err != nil {
		return nil, err
	}

	rootDisk, err := disk.IsRootDisk(path)
	if err != nil {
		return nil, err
	}

	p := &xlStorage{
		diskPath: path,
		endpoint: ep,
		pool: sync.Pool{
			New: func() interface{} {
				b := disk.AlignedBlock(readBlockSize)
				return &b
			},
		},
		globalSync: env.Get(config.EnvFSOSync, config.EnableOff) == config.EnableOn,
		// Allow disk usage crawler to run with up to 2 concurrent
		// I/O ops, if and when activeIOCount reaches this
		// value disk usage routine suspends the crawler
		// and waits until activeIOCount reaches below this threshold.
		maxActiveIOCount: 3,
		ctx:              GlobalContext,
		rootDisk:         rootDisk,
	}

	// Success.
	return p, nil
}

// getDiskInfo returns given disk information.
func getDiskInfo(diskPath string) (di disk.Info, err error) {
	if err = checkPathLength(diskPath); err == nil {
		di, err = disk.GetInfo(diskPath)
	}

	switch {
	case os.IsNotExist(err):
		err = errDiskNotFound
	case isSysErrTooLong(err):
		err = errFileNameTooLong
	case isSysErrIO(err):
		err = errFaultyDisk
	}

	return di, err
}

// check if disk total has minimum required size.
func checkDiskMinTotal(di disk.Info) (err error) {
	// Remove 5% from total space for cumulative disk space
	// used for journalling, inodes etc.
	totalDiskSpace := float64(di.Total) * diskFillFraction
	if int64(totalDiskSpace) <= diskMinTotalSpace {
		return errMinDiskSize
	}
	return nil
}

// Implements stringer compatible interface.
func (s *xlStorage) String() string {
	return s.diskPath
}

func (s *xlStorage) Hostname() string {
	return s.endpoint.Host
}

func (s *xlStorage) Endpoint() Endpoint {
	return s.endpoint
}

func (*xlStorage) Close() error {
	return nil
}

func (s *xlStorage) IsOnline() bool {
	return true
}

func (s *xlStorage) IsLocal() bool {
	return true
}

func (s *xlStorage) Healing() bool {
	healingFile := pathJoin(s.diskPath, minioMetaBucket,
		bucketMetaPrefix, healingTrackerFilename)
	_, err := os.Stat(healingFile)
	return err == nil
}

func (s *xlStorage) waitForLowActiveIO() {
	max := lowActiveIOWaitMaxN
	for atomic.LoadInt32(&s.activeIOCount) >= s.maxActiveIOCount {
		time.Sleep(lowActiveIOWaitTick)
		max--
		if max == 0 {
			if intDataUpdateTracker.debug {
				logger.Info("waitForLowActiveIO: waited %d times, resuming", lowActiveIOWaitMaxN)
			}
			break
		}
	}
}

func (s *xlStorage) CrawlAndGetDataUsage(ctx context.Context, cache dataUsageCache) (dataUsageCache, error) {
	// Check if the current bucket has a configured lifecycle policy
	lc, err := globalLifecycleSys.Get(cache.Info.Name)
	if err == nil && lc.HasActiveRules("", true) {
		cache.Info.lifeCycle = lc
	}

	// Get object api
	objAPI := newObjectLayerFn()
	if objAPI == nil {
		return cache, errServerNotInitialized
	}
	opts := globalHealConfig

	dataUsageInfo, err := crawlDataFolder(ctx, s.diskPath, cache, func(item crawlItem) (int64, error) {
		// Look for `xl.meta/xl.json' at the leaf.
		if !strings.HasSuffix(item.Path, SlashSeparator+xlStorageFormatFile) &&
			!strings.HasSuffix(item.Path, SlashSeparator+xlStorageFormatFileV1) {
			// if no xl.meta/xl.json found, skip the file.
			return 0, errSkipFile
		}

		buf, err := ioutil.ReadFile(item.Path)
		if err != nil {
			return 0, errSkipFile
		}

		// Remove filename which is the meta file.
		item.transformMetaDir()

		fivs, err := getFileInfoVersions(buf, item.bucket, item.objectPath())
		if err != nil {
			return 0, errSkipFile
		}

		var totalSize int64
		var numVersions = len(fivs.Versions)

		for i, version := range fivs.Versions {
			var successorModTime time.Time
			if i > 0 {
				successorModTime = fivs.Versions[i-1].ModTime
			}
			oi := version.ToObjectInfo(item.bucket, item.objectPath())
			size := item.applyActions(ctx, objAPI, actionMeta{
				numVersions:      numVersions,
				successorModTime: successorModTime,
				oi:               oi,
			})
			if !version.Deleted {
				// Bitrot check local data
				if size > 0 && item.heal && opts.Bitrot {
					s.waitForLowActiveIO()
					err := s.VerifyFile(ctx, item.bucket, item.objectPath(), version)
					switch err {
					case errFileCorrupt:
						res, err := objAPI.HealObject(ctx, item.bucket, item.objectPath(), oi.VersionID, madmin.HealOpts{
							Remove:   healDeleteDangling,
							ScanMode: madmin.HealDeepScan,
						})
						if err != nil {
							if !errors.Is(err, NotImplemented{}) {
								logger.LogIf(ctx, err)
							}
							size = 0
						} else {
							size = res.ObjectSize
						}
					default:
						// VerifyFile already logs errors
					}
				}
				totalSize += size
			}
			item.healReplication(ctx, objAPI, actionMeta{oi: oi})
		}
		return totalSize, nil
	})

	if err != nil {
		return dataUsageInfo, err
	}

	dataUsageInfo.Info.LastUpdate = time.Now()
	return dataUsageInfo, nil
}

// DiskInfo is an extended type which returns current
// disk usage per path.
type DiskInfo struct {
	Total     uint64
	Free      uint64
	Used      uint64
	FSType    string
	RootDisk  bool
	Healing   bool
	Endpoint  string
	MountPath string
	ID        string
	Error     string // carries the error over the network
}

// DiskInfo provides current information about disk space usage,
// total free inodes and underlying filesystem.
func (s *xlStorage) DiskInfo(context.Context) (info DiskInfo, err error) {
	atomic.AddInt32(&s.activeIOCount, 1)
	defer func() {
		atomic.AddInt32(&s.activeIOCount, -1)
	}()

	s.diskInfoCache.Once.Do(func() {
		s.diskInfoCache.TTL = time.Second
		s.diskInfoCache.Update = func() (interface{}, error) {
			dcinfo := DiskInfo{
				RootDisk:  s.rootDisk,
				MountPath: s.diskPath,
				Endpoint:  s.endpoint.String(),
			}
			di, err := getDiskInfo(s.diskPath)
			if err != nil {
				return dcinfo, err
			}
			dcinfo.Total = di.Total
			dcinfo.Free = di.Free
			dcinfo.Used = di.Used
			dcinfo.FSType = di.FSType

			diskID, err := s.GetDiskID()
			if errors.Is(err, errUnformattedDisk) {
				// if we found an unformatted disk then
				// healing is automatically true.
				dcinfo.Healing = true
			} else {
				// Check if the disk is being healed if GetDiskID
				// returned any error other than fresh disk
				dcinfo.Healing = s.Healing()
			}

			dcinfo.ID = diskID
			return dcinfo, err
		}
	})

	v, err := s.diskInfoCache.Get()
	info = v.(DiskInfo)
	return info, err
}

// getVolDir - will convert incoming volume names to
// corresponding valid volume names on the backend in a platform
// compatible way for all operating systems. If volume is not found
// an error is generated.
func (s *xlStorage) getVolDir(volume string) (string, error) {
	if volume == "" || volume == "." || volume == ".." {
		return "", errVolumeNotFound
	}
	volumeDir := pathJoin(s.diskPath, volume)
	return volumeDir, nil
}

// GetDiskID - returns the cached disk uuid
func (s *xlStorage) GetDiskID() (string, error) {
	s.RLock()
	diskID := s.diskID
	fileInfo := s.formatFileInfo
	lastCheck := s.formatLastCheck
	s.RUnlock()

	// check if we have a valid disk ID that is less than 1 second old.
	if fileInfo != nil && diskID != "" && time.Since(lastCheck) <= time.Second {
		return diskID, nil
	}

	s.Lock()
	defer s.Unlock()

	// If somebody else updated the disk ID and changed the time, return what they got.
	if !lastCheck.IsZero() && !s.formatLastCheck.Equal(lastCheck) && diskID != "" {
		// Somebody else got the lock first.
		return diskID, nil
	}

	formatFile := pathJoin(s.diskPath, minioMetaBucket, formatConfigFile)
	fi, err := os.Stat(formatFile)
	if err != nil {
		// If the disk is still not initialized.
		if os.IsNotExist(err) {
			_, err = os.Stat(s.diskPath)
			if err == nil {
				// Disk is present but missing `format.json`
				return "", errUnformattedDisk
			}
			if os.IsNotExist(err) {
				return "", errDiskNotFound
			} else if os.IsPermission(err) {
				return "", errDiskAccessDenied
			}
			logger.LogIf(GlobalContext, err) // log unexpected errors
			return "", errCorruptedFormat
		} else if os.IsPermission(err) {
			return "", errDiskAccessDenied
		}
		logger.LogIf(GlobalContext, err) // log unexpected errors
		return "", errCorruptedFormat
	}

	if xioutil.SameFile(fi, fileInfo) && diskID != "" {
		// If the file has not changed, just return the cached diskID information.
		s.formatLastCheck = time.Now()
		return diskID, nil
	}

	b, err := ioutil.ReadFile(formatFile)
	if err != nil {
		// If the disk is still not initialized.
		if os.IsNotExist(err) {
			_, err = os.Stat(s.diskPath)
			if err == nil {
				// Disk is present but missing `format.json`
				return "", errUnformattedDisk
			}
			if os.IsNotExist(err) {
				return "", errDiskNotFound
			} else if os.IsPermission(err) {
				return "", errDiskAccessDenied
			}
			logger.LogIf(GlobalContext, err) // log unexpected errors
			return "", errCorruptedFormat
		} else if os.IsPermission(err) {
			return "", errDiskAccessDenied
		}
		logger.LogIf(GlobalContext, err) // log unexpected errors
		return "", errCorruptedFormat
	}

	format := &formatErasureV3{}
	var json = jsoniter.ConfigCompatibleWithStandardLibrary
	if err = json.Unmarshal(b, &format); err != nil {
		logger.LogIf(GlobalContext, err) // log unexpected errors
		return "", errCorruptedFormat
	}

	s.diskID = format.Erasure.This
	s.formatFileInfo = fi
	s.formatLastCheck = time.Now()
	return s.diskID, nil
}

// Make a volume entry.
func (s *xlStorage) SetDiskID(id string) {
	// NO-OP for xlStorage as it is handled either by xlStorageDiskIDCheck{} for local disks or
	// storage rest server for remote disks.
}

func (s *xlStorage) MakeVolBulk(ctx context.Context, volumes ...string) (err error) {
	for _, volume := range volumes {
		if err = s.MakeVol(ctx, volume); err != nil {
			if os.IsPermission(err) {
				return errVolumeAccessDenied
			}
		}
	}
	return nil
}

// Make a volume entry.
func (s *xlStorage) MakeVol(ctx context.Context, volume string) (err error) {
	if !isValidVolname(volume) {
		return errInvalidArgument
	}

	atomic.AddInt32(&s.activeIOCount, 1)
	defer func() {
		atomic.AddInt32(&s.activeIOCount, -1)
	}()

	volumeDir, err := s.getVolDir(volume)
	if err != nil {
		return err
	}

	if _, err := os.Stat(volumeDir); err != nil {
		// Volume does not exist we proceed to create.
		if os.IsNotExist(err) {
			// Make a volume entry, with mode 0777 mkdir honors system umask.
			err = os.MkdirAll(volumeDir, 0777)
		}
		if os.IsPermission(err) {
			return errDiskAccessDenied
		} else if isSysErrIO(err) {
			return errFaultyDisk
		}
		return err
	}

	// Stat succeeds we return errVolumeExists.
	return errVolumeExists
}

// ListVols - list volumes.
func (s *xlStorage) ListVols(context.Context) (volsInfo []VolInfo, err error) {
	atomic.AddInt32(&s.activeIOCount, 1)
	defer func() {
		atomic.AddInt32(&s.activeIOCount, -1)
	}()

	return listVols(s.diskPath)
}

// List all the volumes from diskPath.
func listVols(dirPath string) ([]VolInfo, error) {
	if err := checkPathLength(dirPath); err != nil {
		return nil, err
	}
	entries, err := readDir(dirPath)
	if err != nil {
		return nil, errDiskNotFound
	}
	volsInfo := make([]VolInfo, 0, len(entries))
	for _, entry := range entries {
		if !HasSuffix(entry, SlashSeparator) || !isValidVolname(slashpath.Clean(entry)) {
			// Skip if entry is neither a directory not a valid volume name.
			continue
		}
		var fi os.FileInfo
		fi, err = os.Stat(pathJoin(dirPath, entry))
		if err != nil {
			// If the file does not exist, skip the entry.
			if os.IsNotExist(err) {
				continue
			} else if isSysErrIO(err) {
				return nil, errFaultyDisk
			}
			return nil, err
		}
		volsInfo = append(volsInfo, VolInfo{
			Name: fi.Name(),
			// As os.Stat() doesn't carry other than ModTime(), use
			// ModTime() as CreatedTime.
			Created: fi.ModTime(),
		})
	}
	return volsInfo, nil
}

// StatVol - get volume info.
func (s *xlStorage) StatVol(ctx context.Context, volume string) (vol VolInfo, err error) {
	atomic.AddInt32(&s.activeIOCount, 1)
	defer func() {
		atomic.AddInt32(&s.activeIOCount, -1)
	}()

	// Verify if volume is valid and it exists.
	volumeDir, err := s.getVolDir(volume)
	if err != nil {
		return VolInfo{}, err
	}
	// Stat a volume entry.
	var st os.FileInfo
	st, err = os.Stat(volumeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return VolInfo{}, errVolumeNotFound
		} else if isSysErrIO(err) {
			return VolInfo{}, errFaultyDisk
		}
		return VolInfo{}, err
	}
	// As os.Stat() doesn't carry other than ModTime(), use ModTime()
	// as CreatedTime.
	createdTime := st.ModTime()
	return VolInfo{
		Name:    volume,
		Created: createdTime,
	}, nil
}

// DeleteVol - delete a volume.
func (s *xlStorage) DeleteVol(ctx context.Context, volume string, forceDelete bool) (err error) {
	atomic.AddInt32(&s.activeIOCount, 1)
	defer func() {
		atomic.AddInt32(&s.activeIOCount, -1)
	}()

	// Verify if volume is valid and it exists.
	volumeDir, err := s.getVolDir(volume)
	if err != nil {
		return err
	}

	if forceDelete {
		err = os.RemoveAll(volumeDir)
	} else {
		err = os.Remove(volumeDir)
	}

	if err != nil {
		switch {
		case os.IsNotExist(err):
			return errVolumeNotFound
		case isSysErrNotEmpty(err):
			return errVolumeNotEmpty
		case os.IsPermission(err):
			return errDiskAccessDenied
		case isSysErrIO(err):
			return errFaultyDisk
		default:
			return err
		}
	}
	return nil
}

const guidSplunk = "guidSplunk"

// ListDirSplunk - return all the entries at the given directory path.
// If an entry is a directory it will be returned with a trailing SlashSeparator.
func (s *xlStorage) ListDirSplunk(volume, dirPath string, count int) (entries []string, err error) {
	guidIndex := strings.Index(dirPath, guidSplunk)
	if guidIndex != -1 {
		return nil, nil
	}

	atomic.AddInt32(&s.activeIOCount, 1)
	defer func() {
		atomic.AddInt32(&s.activeIOCount, -1)
	}()

	// Verify if volume is valid and it exists.
	volumeDir, err := s.getVolDir(volume)
	if err != nil {
		return nil, err
	}

	if _, err = os.Stat(volumeDir); err != nil {
		if os.IsNotExist(err) {
			return nil, errVolumeNotFound
		} else if isSysErrIO(err) {
			return nil, errFaultyDisk
		}
		return nil, err
	}

	dirPathAbs := pathJoin(volumeDir, dirPath)
	if count > 0 {
		entries, err = readDirN(dirPathAbs, count)
	} else {
		entries, err = readDir(dirPathAbs)
	}
	if err != nil {
		return nil, err
	}

	return entries, nil
}

func (s *xlStorage) isLeafSplunk(volume string, leafPath string) bool {
	const receiptJSON = "receipt.json"

	if path.Base(leafPath) != receiptJSON {
		return false
	}
	return s.isLeaf(volume, leafPath)
}

func (s *xlStorage) isLeaf(volume string, leafPath string) bool {
	volumeDir, err := s.getVolDir(volume)
	if err != nil {
		return false
	}

	_, err = os.Stat(pathJoin(volumeDir, leafPath, xlStorageFormatFile))
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		// We need a fallback code where directory might contain
		// legacy `xl.json`, in such situation we just rename
		// and proceed if rename is successful we know that it
		// is the leaf since `xl.json` was present.
		return s.renameLegacyMetadata(volume, leafPath) == nil
	}
	return false
}

func (s *xlStorage) isLeafDir(volume, leafPath string) bool {
	volumeDir, err := s.getVolDir(volume)
	if err != nil {
		return false
	}

	return isDirEmpty(pathJoin(volumeDir, leafPath))
}

// WalkSplunk - is a sorted walker which returns file entries in lexically
// sorted order, additionally along with metadata about each of those entries.
// Implemented specifically for Splunk backend structure and List call with
// delimiter as "guidSplunk"
func (s *xlStorage) WalkSplunk(ctx context.Context, volume, dirPath, marker string, endWalkCh <-chan struct{}) (ch chan FileInfo, err error) {
	// Verify if volume is valid and it exists.
	volumeDir, err := s.getVolDir(volume)
	if err != nil {
		return nil, err
	}

	// Stat a volume entry.
	_, err = os.Stat(volumeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errVolumeNotFound
		} else if isSysErrIO(err) {
			return nil, errFaultyDisk
		}
		return nil, err
	}

	ch = make(chan FileInfo)
	go func() {
		defer close(ch)
		listDir := func(volume, dirPath, dirEntry string) (emptyDir bool, entries []string, delayIsLeaf bool) {
			entries, err := s.ListDirSplunk(volume, dirPath, -1)
			if err != nil {
				return false, nil, false
			}
			if len(entries) == 0 {
				return true, nil, false
			}
			entries, delayIsLeaf = filterListEntries(volume, dirPath, entries, dirEntry, s.isLeafSplunk)
			return false, entries, delayIsLeaf
		}

		walkResultCh := startTreeWalk(GlobalContext, volume, dirPath, marker, true, listDir, s.isLeafSplunk, s.isLeafDir, endWalkCh)
		for walkResult := range walkResultCh {
			var fi FileInfo
			if HasSuffix(walkResult.entry, SlashSeparator) {
				fi = FileInfo{
					Volume: volume,
					Name:   walkResult.entry,
					Mode:   os.ModeDir,
				}
			} else {
				var err error
				var xlMetaBuf []byte
				xlMetaBuf, err = ioutil.ReadFile(pathJoin(volumeDir, walkResult.entry, xlStorageFormatFile))
				if err != nil {
					continue
				}
				fi, err = getFileInfo(xlMetaBuf, volume, walkResult.entry, "")
				if err != nil {
					continue
				}
				if fi.Deleted {
					// Ignore delete markers.
					continue
				}
			}
			select {
			case ch <- fi:
			case <-endWalkCh:
				return
			}
		}
	}()

	return ch, nil
}

// WalkVersions - is a sorted walker which returns file entries in lexically sorted order,
// additionally along with metadata version info about each of those entries.
func (s *xlStorage) WalkVersions(ctx context.Context, volume, dirPath, marker string, recursive bool, endWalkCh <-chan struct{}) (ch chan FileInfoVersions, err error) {
	atomic.AddInt32(&s.activeIOCount, 1)
	defer func() {
		atomic.AddInt32(&s.activeIOCount, -1)
	}()

	// Verify if volume is valid and it exists.
	volumeDir, err := s.getVolDir(volume)
	if err != nil {
		return nil, err
	}

	// Stat a volume entry.
	_, err = os.Stat(volumeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errVolumeNotFound
		} else if isSysErrIO(err) {
			return nil, errFaultyDisk
		}
		return nil, err
	}

	// Fast exit track to check if we are listing an object with
	// a trailing slash, this will avoid to list the object content.
	if HasSuffix(dirPath, SlashSeparator) {
		if st, err := os.Stat(pathJoin(volumeDir, dirPath, xlStorageFormatFile)); err == nil && st.Mode().IsRegular() {
			return nil, errFileNotFound
		}
	}

	ch = make(chan FileInfoVersions)
	go func() {
		defer close(ch)
		listDir := func(volume, dirPath, dirEntry string) (emptyDir bool, entries []string, delayIsLeaf bool) {
			entries, err := s.ListDir(ctx, volume, dirPath, -1)
			if err != nil {
				return false, nil, false
			}
			if len(entries) == 0 {
				return true, nil, false
			}
			entries, delayIsLeaf = filterListEntries(volume, dirPath, entries, dirEntry, s.isLeaf)
			return false, entries, delayIsLeaf
		}

		walkResultCh := startTreeWalk(GlobalContext, volume, dirPath, marker, recursive, listDir, s.isLeaf, s.isLeafDir, endWalkCh)
		for walkResult := range walkResultCh {
			var fiv FileInfoVersions
			if HasSuffix(walkResult.entry, SlashSeparator) {
				fiv = FileInfoVersions{
					Volume: volume,
					Name:   walkResult.entry,
					Versions: []FileInfo{
						{
							Volume: volume,
							Name:   walkResult.entry,
							Mode:   os.ModeDir,
						},
					},
				}
			} else {
				xlMetaBuf, err := ioutil.ReadFile(pathJoin(volumeDir, walkResult.entry, xlStorageFormatFile))
				if err != nil {
					continue
				}

				fiv, err = getFileInfoVersions(xlMetaBuf, volume, walkResult.entry)
				if err != nil {
					continue
				}
			}
			select {
			case ch <- fiv:
			case <-endWalkCh:
				return
			}
		}
	}()

	return ch, nil
}

// Walk - is a sorted walker which returns file entries in lexically
// sorted order, additionally along with metadata about each of those entries.
func (s *xlStorage) Walk(ctx context.Context, volume, dirPath, marker string, recursive bool, endWalkCh <-chan struct{}) (ch chan FileInfo, err error) {
	atomic.AddInt32(&s.activeIOCount, 1)
	defer func() {
		atomic.AddInt32(&s.activeIOCount, -1)
	}()

	// Verify if volume is valid and it exists.
	volumeDir, err := s.getVolDir(volume)
	if err != nil {
		return nil, err
	}

	// Stat a volume entry.
	_, err = os.Stat(volumeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errVolumeNotFound
		} else if isSysErrIO(err) {
			return nil, errFaultyDisk
		}
		return nil, err
	}

	// Fast exit track to check if we are listing an object with
	// a trailing slash, this will avoid to list the object content.
	if HasSuffix(dirPath, SlashSeparator) {
		if st, err := os.Stat(pathJoin(volumeDir, dirPath, xlStorageFormatFile)); err == nil && st.Mode().IsRegular() {
			return nil, errFileNotFound
		}
	}

	ch = make(chan FileInfo)
	go func() {
		defer close(ch)
		listDir := func(volume, dirPath, dirEntry string) (emptyDir bool, entries []string, delayIsLeaf bool) {
			entries, err := s.ListDir(ctx, volume, dirPath, -1)
			if err != nil {
				return false, nil, false
			}
			if len(entries) == 0 {
				return true, nil, false
			}
			entries, delayIsLeaf = filterListEntries(volume, dirPath, entries, dirEntry, s.isLeaf)
			return false, entries, delayIsLeaf
		}

		walkResultCh := startTreeWalk(GlobalContext, volume, dirPath, marker, recursive, listDir, s.isLeaf, s.isLeafDir, endWalkCh)
		for walkResult := range walkResultCh {
			var fi FileInfo
			if HasSuffix(walkResult.entry, SlashSeparator) {
				fi = FileInfo{
					Volume: volume,
					Name:   walkResult.entry,
					Mode:   os.ModeDir,
				}
			} else {
				var err error
				var xlMetaBuf []byte
				xlMetaBuf, err = ioutil.ReadFile(pathJoin(volumeDir, walkResult.entry, xlStorageFormatFile))
				if err != nil {
					continue
				}
				fi, err = getFileInfo(xlMetaBuf, volume, walkResult.entry, "")
				if err != nil {
					continue
				}
				if fi.Deleted {
					// Ignore delete markers.
					continue
				}
			}
			select {
			case ch <- fi:
			case <-endWalkCh:
				return
			}
		}
	}()

	return ch, nil
}

// ListDir - return all the entries at the given directory path.
// If an entry is a directory it will be returned with a trailing SlashSeparator.
func (s *xlStorage) ListDir(ctx context.Context, volume, dirPath string, count int) (entries []string, err error) {
	atomic.AddInt32(&s.activeIOCount, 1)
	defer func() {
		atomic.AddInt32(&s.activeIOCount, -1)
	}()

	// Verify if volume is valid and it exists.
	volumeDir, err := s.getVolDir(volume)
	if err != nil {
		return nil, err
	}

	if _, err = os.Stat(volumeDir); err != nil {
		if os.IsNotExist(err) {
			return nil, errVolumeNotFound
		} else if isSysErrIO(err) {
			return nil, errFaultyDisk
		}
		return nil, err
	}

	dirPathAbs := pathJoin(volumeDir, dirPath)
	if count > 0 {
		entries, err = readDirN(dirPathAbs, count)
	} else {
		entries, err = readDir(dirPathAbs)
	}
	if err != nil {
		return nil, err
	}

	return entries, nil
}

// DeleteVersions deletes slice of versions, it can be same object
// or multiple objects.
func (s *xlStorage) DeleteVersions(ctx context.Context, volume string, versions []FileInfo) []error {
	errs := make([]error, len(versions))
	for i, version := range versions {
		if err := s.DeleteVersion(ctx, volume, version.Name, version); err != nil {
			errs[i] = err
		}
	}

	return errs
}

// DeleteVersion - deletes FileInfo metadata for path at `xl.meta`
func (s *xlStorage) DeleteVersion(ctx context.Context, volume, path string, fi FileInfo) error {
	if HasSuffix(path, SlashSeparator) {
		return s.Delete(ctx, volume, path, false)
	}

	buf, err := s.ReadAll(ctx, volume, pathJoin(path, xlStorageFormatFile))
	if err != nil {
		return err
	}

	if len(buf) == 0 {
		return errFileNotFound
	}

	atomic.AddInt32(&s.activeIOCount, 1)
	defer func() {
		atomic.AddInt32(&s.activeIOCount, -1)
	}()

	volumeDir, err := s.getVolDir(volume)
	if err != nil {
		return err
	}

	if !isXL2V1Format(buf) {
		// Delete the meta file, if there are no more versions the
		// top level parent is automatically removed.
		return deleteFile(volumeDir, pathJoin(volumeDir, path), true)
	}

	var xlMeta xlMetaV2
	if err = xlMeta.Load(buf); err != nil {
		return err
	}

	dataDir, lastVersion, err := xlMeta.DeleteVersion(fi)
	if err != nil {
		return err
	}

	buf, err = xlMeta.MarshalMsg(append(xlHeader[:], xlVersionV1[:]...))
	if err != nil {
		return err
	}

	// when data-dir is specified.
	if dataDir != "" {
		filePath := pathJoin(volumeDir, path, dataDir)
		if err = checkPathLength(filePath); err != nil {
			return err
		}

		if err = removeAll(filePath); err != nil {
			return err
		}
	}

	if !lastVersion {
		return s.WriteAll(ctx, volume, pathJoin(path, xlStorageFormatFile), bytes.NewReader(buf))
	}

	// Delete the meta file, if there are no more versions the
	// top level parent is automatically removed.
	filePath := pathJoin(volumeDir, path, xlStorageFormatFile)
	if err = checkPathLength(filePath); err != nil {
		return err
	}

	return deleteFile(volumeDir, filePath, false)
}

// WriteMetadata - writes FileInfo metadata for path at `xl.meta`
func (s *xlStorage) WriteMetadata(ctx context.Context, volume, path string, fi FileInfo) error {
	buf, err := s.ReadAll(ctx, volume, pathJoin(path, xlStorageFormatFile))
	if err != nil && err != errFileNotFound {
		return err
	}

	atomic.AddInt32(&s.activeIOCount, 1)
	defer func() {
		atomic.AddInt32(&s.activeIOCount, -1)
	}()

	var xlMeta xlMetaV2
	if !isXL2V1Format(buf) {
		xlMeta, err = newXLMetaV2(fi)
		if err != nil {
			return err
		}
		buf, err = xlMeta.MarshalMsg(append(xlHeader[:], xlVersionV1[:]...))
		if err != nil {
			return err
		}
	} else {
		if err = xlMeta.Load(buf); err != nil {
			return err
		}
		if err = xlMeta.AddVersion(fi); err != nil {
			return err
		}
		buf, err = xlMeta.MarshalMsg(append(xlHeader[:], xlVersionV1[:]...))
		if err != nil {
			return err
		}
	}

	return s.WriteAll(ctx, volume, pathJoin(path, xlStorageFormatFile), bytes.NewReader(buf))
}

func (s *xlStorage) renameLegacyMetadata(volume, path string) error {
	atomic.AddInt32(&s.activeIOCount, 1)
	defer func() {
		atomic.AddInt32(&s.activeIOCount, -1)
	}()

	volumeDir, err := s.getVolDir(volume)
	if err != nil {
		return err
	}

	//gi Stat a volume entry.
	_, err = os.Stat(volumeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return errVolumeNotFound
		} else if isSysErrIO(err) {
			return errFaultyDisk
		} else if isSysErrTooManyFiles(err) {
			return errTooManyOpenFiles
		}
		return err
	}

	// Validate file path length, before reading.
	filePath := pathJoin(volumeDir, path)
	if err = checkPathLength(filePath); err != nil {
		return err
	}

	srcFilePath := pathJoin(filePath, xlStorageFormatFileV1)
	dstFilePath := pathJoin(filePath, xlStorageFormatFile)

	// Renaming xl.json to xl.meta should be fully synced to disk.
	defer func() {
		if err == nil {
			if s.globalSync {
				// Sync to disk only upon success.
				globalSync()
			}
		}
	}()

	if err = os.Rename(srcFilePath, dstFilePath); err != nil {
		switch {
		case isSysErrNotDir(err):
			return errFileNotFound
		case isSysErrPathNotFound(err):
			return errFileNotFound
		case isSysErrCrossDevice(err):
			return fmt.Errorf("%w (%s)->(%s)", errCrossDeviceLink, srcFilePath, dstFilePath)
		case os.IsNotExist(err):
			return errFileNotFound
		case os.IsExist(err):
			// This is returned only when destination is a directory and we
			// are attempting a rename from file to directory.
			return errIsNotRegular
		default:
			return err
		}
	}
	return nil
}

// ReadVersion - reads metadata and returns FileInfo at path `xl.meta`
func (s *xlStorage) ReadVersion(ctx context.Context, volume, path, versionID string, checkDataDir bool) (fi FileInfo, err error) {
	buf, err := s.ReadAll(ctx, volume, pathJoin(path, xlStorageFormatFile))
	if err != nil {
		if err == errFileNotFound {
			if err = s.renameLegacyMetadata(volume, path); err != nil {
				return fi, err
			}
			buf, err = s.ReadAll(ctx, volume, pathJoin(path, xlStorageFormatFile))
			if err != nil {
				return fi, err
			}
		} else {
			return fi, err
		}
	}

	if len(buf) == 0 {
		if versionID != "" {
			return fi, errFileVersionNotFound
		}
		return fi, errFileNotFound
	}

	fi, err = getFileInfo(buf, volume, path, versionID)
	if err != nil {
		return fi, err
	}

	if fi.DataDir != "" && checkDataDir {
		if _, err = s.StatVol(ctx, pathJoin(volume, path, fi.DataDir, slashSeparator)); err != nil {
			if err == errVolumeNotFound {
				if versionID != "" {
					return fi, errFileVersionNotFound
				}
				return fi, errFileNotFound
			}
			return fi, err
		}
	}

	return fi, nil
}

// ReadAll reads from r until an error or EOF and returns the data it read.
// A successful call returns err == nil, not err == EOF. Because ReadAll is
// defined to read from src until EOF, it does not treat an EOF from Read
// as an error to be reported.
// This API is meant to be used on files which have small memory footprint, do
// not use this on large files as it would cause server to crash.
func (s *xlStorage) ReadAll(ctx context.Context, volume string, path string) (buf []byte, err error) {
	atomic.AddInt32(&s.activeIOCount, 1)
	defer func() {
		atomic.AddInt32(&s.activeIOCount, -1)
	}()

	volumeDir, err := s.getVolDir(volume)
	if err != nil {
		return nil, err
	}

	// Stat a volume entry.
	_, err = os.Stat(volumeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errVolumeNotFound
		} else if isSysErrIO(err) {
			return nil, errFaultyDisk
		} else if isSysErrTooManyFiles(err) {
			return nil, errTooManyOpenFiles
		}
		return nil, err
	}

	// Validate file path length, before reading.
	filePath := pathJoin(volumeDir, path)
	if err = checkPathLength(filePath); err != nil {
		return nil, err
	}

	// Open the file for reading.
	buf, err = ioutil.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errFileNotFound
		} else if os.IsPermission(err) {
			return nil, errFileAccessDenied
		} else if errors.Is(err, syscall.ENOTDIR) || errors.Is(err, syscall.EISDIR) {
			return nil, errFileNotFound
		} else if isSysErrHandleInvalid(err) {
			// This case is special and needs to be handled for windows.
			return nil, errFileNotFound
		} else if isSysErrIO(err) {
			return nil, errFaultyDisk
		}
		return nil, err
	}
	return buf, nil
}

// ReadFile reads exactly len(buf) bytes into buf. It returns the
// number of bytes copied. The error is EOF only if no bytes were
// read. On return, n == len(buf) if and only if err == nil. n == 0
// for io.EOF.
//
// If an EOF happens after reading some but not all the bytes,
// ReadFile returns ErrUnexpectedEOF.
//
// If the BitrotVerifier is not nil or not verified ReadFile
// tries to verify whether the disk has bitrot.
//
// Additionally ReadFile also starts reading from an offset. ReadFile
// semantics are same as io.ReadFull.
func (s *xlStorage) ReadFile(ctx context.Context, volume string, path string, offset int64, buffer []byte, verifier *BitrotVerifier) (int64, error) {
	if offset < 0 {
		return 0, errInvalidArgument
	}

	atomic.AddInt32(&s.activeIOCount, 1)
	defer func() {
		atomic.AddInt32(&s.activeIOCount, -1)
	}()

	volumeDir, err := s.getVolDir(volume)
	if err != nil {
		return 0, err
	}

	var n int

	// Stat a volume entry.
	_, err = os.Stat(volumeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, errVolumeNotFound
		} else if isSysErrIO(err) {
			return 0, errFaultyDisk
		}
		return 0, err
	}

	// Validate effective path length before reading.
	filePath := pathJoin(volumeDir, path)
	if err = checkPathLength(filePath); err != nil {
		return 0, err
	}

	// Open the file for reading.
	file, err := os.Open(filePath)
	if err != nil {
		switch {
		case os.IsNotExist(err):
			return 0, errFileNotFound
		case os.IsPermission(err):
			return 0, errFileAccessDenied
		case isSysErrNotDir(err):
			return 0, errFileAccessDenied
		case isSysErrIO(err):
			return 0, errFaultyDisk
		case isSysErrTooManyFiles(err):
			return 0, errTooManyOpenFiles
		default:
			return 0, err
		}
	}

	// Close the file descriptor.
	defer file.Close()

	st, err := file.Stat()
	if err != nil {
		return 0, err
	}

	// Verify it is a regular file, otherwise subsequent Seek is
	// undefined.
	if !st.Mode().IsRegular() {
		return 0, errIsNotRegular
	}

	if verifier == nil {
		n, err = file.ReadAt(buffer, offset)
		return int64(n), err
	}

	bufp := s.pool.Get().(*[]byte)
	defer s.pool.Put(bufp)

	h := verifier.algorithm.New()
	if _, err = io.CopyBuffer(h, io.LimitReader(file, offset), *bufp); err != nil {
		return 0, err
	}

	if n, err = io.ReadFull(file, buffer); err != nil {
		return int64(n), err
	}

	if _, err = h.Write(buffer); err != nil {
		return 0, err
	}

	if _, err = io.CopyBuffer(h, file, *bufp); err != nil {
		return 0, err
	}

	if !bytes.Equal(h.Sum(nil), verifier.sum) {
		return 0, errFileCorrupt
	}

	return int64(len(buffer)), nil
}

func (s *xlStorage) openFile(volume, path string, mode int) (f *os.File, err error) {
	volumeDir, err := s.getVolDir(volume)
	if err != nil {
		return nil, err
	}
	// Stat a volume entry.
	_, err = os.Stat(volumeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errVolumeNotFound
		} else if isSysErrIO(err) {
			return nil, errFaultyDisk
		}
		return nil, err
	}

	filePath := pathJoin(volumeDir, path)
	if err = checkPathLength(filePath); err != nil {
		return nil, err
	}

	// Verify if the file already exists and is not of regular type.
	var st os.FileInfo
	if st, err = os.Stat(filePath); err == nil {
		if !st.Mode().IsRegular() {
			return nil, errIsNotRegular
		}
	} else {
		// Create top level directories if they don't exist.
		// with mode 0777 mkdir honors system umask.
		if err = mkdirAll(slashpath.Dir(filePath), 0777); err != nil {
			return nil, err
		}
	}

	w, err := os.OpenFile(filePath, mode, 0666)
	if err != nil {
		// File path cannot be verified since one of the parents is a file.
		switch {
		case isSysErrNotDir(err):
			return nil, errFileAccessDenied
		case os.IsPermission(err):
			return nil, errFileAccessDenied
		case isSysErrIO(err):
			return nil, errFaultyDisk
		case isSysErrTooManyFiles(err):
			return nil, errTooManyOpenFiles
		default:
			return nil, err
		}
	}

	return w, nil
}

// ReadFileStream - Returns the read stream of the file.
func (s *xlStorage) ReadFileStream(ctx context.Context, volume, path string, offset, length int64) (io.ReadCloser, error) {
	if offset < 0 {
		return nil, errInvalidArgument
	}

	volumeDir, err := s.getVolDir(volume)
	if err != nil {
		return nil, err
	}
	// Stat a volume entry.
	_, err = os.Stat(volumeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errVolumeNotFound
		} else if isSysErrIO(err) {
			return nil, errFaultyDisk
		}
		return nil, err
	}

	// Validate effective path length before reading.
	filePath := pathJoin(volumeDir, path)
	if err = checkPathLength(filePath); err != nil {
		return nil, err
	}

	// Open the file for reading.
	file, err := os.Open(filePath)
	if err != nil {
		switch {
		case os.IsNotExist(err):
			return nil, errFileNotFound
		case os.IsPermission(err):
			return nil, errFileAccessDenied
		case isSysErrNotDir(err):
			return nil, errFileAccessDenied
		case isSysErrIO(err):
			return nil, errFaultyDisk
		case isSysErrTooManyFiles(err):
			return nil, errTooManyOpenFiles
		default:
			return nil, err
		}
	}

	st, err := file.Stat()
	if err != nil {
		return nil, err
	}

	// Verify it is a regular file, otherwise subsequent Seek is
	// undefined.
	if !st.Mode().IsRegular() {
		return nil, errIsNotRegular
	}

	if _, err = file.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}

	atomic.AddInt32(&s.activeIOCount, 1)
	r := struct {
		io.Reader
		io.Closer
	}{Reader: io.LimitReader(file, length), Closer: closeWrapper(func() error {
		atomic.AddInt32(&s.activeIOCount, -1)
		return file.Close()
	})}

	// Add readahead to big reads
	if length >= readAheadSize {
		rc, err := readahead.NewReadCloserSize(r, readAheadBuffers, readAheadBufSize)
		if err != nil {
			r.Close()
			return nil, err
		}
		return rc, nil
	}

	// Just add a small 64k buffer.
	r.Reader = bufio.NewReaderSize(r.Reader, 64<<10)
	return r, nil
}

// closeWrapper converts a function to an io.Closer
type closeWrapper func() error

// Close calls the wrapped function.
func (c closeWrapper) Close() error {
	return c()
}

// CreateFile - creates the file.
func (s *xlStorage) CreateFile(ctx context.Context, volume, path string, fileSize int64, r io.Reader) (err error) {
	if fileSize < -1 {
		return errInvalidArgument
	}

	atomic.AddInt32(&s.activeIOCount, 1)
	defer func() {
		atomic.AddInt32(&s.activeIOCount, -1)
	}()

	volumeDir, err := s.getVolDir(volume)
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

	filePath := pathJoin(volumeDir, path)
	if err = checkPathLength(filePath); err != nil {
		return err
	}

	// Create top level directories if they don't exist.
	// with mode 0777 mkdir honors system umask.
	if err = mkdirAll(slashpath.Dir(filePath), 0777); err != nil {
		switch {
		case os.IsPermission(err):
			return errFileAccessDenied
		case os.IsExist(err):
			return errFileAccessDenied
		case isSysErrIO(err):
			return errFaultyDisk
		case isSysErrInvalidArg(err):
			return errUnsupportedDisk
		case isSysErrNoSpace(err):
			return errDiskFull
		}
		return err
	}

	w, err := disk.OpenFileDirectIO(filePath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0666)
	if err != nil {
		switch {
		case os.IsPermission(err):
			return errFileAccessDenied
		case os.IsExist(err):
			return errFileAccessDenied
		case isSysErrIO(err):
			return errFaultyDisk
		case isSysErrInvalidArg(err):
			return errUnsupportedDisk
		case isSysErrNoSpace(err):
			return errDiskFull
		default:
			return err
		}
	}

	var e error
	if fileSize > 0 {
		// Allocate needed disk space to append data
		e = Fallocate(int(w.Fd()), 0, fileSize)
	}

	// Ignore errors when Fallocate is not supported in the current system
	if e != nil && !isSysErrNoSys(e) && !isSysErrOpNotSupported(e) {
		switch {
		case isSysErrNoSpace(e):
			err = errDiskFull
		case isSysErrIO(e):
			err = errFaultyDisk
		default:
			// For errors: EBADF, EINTR, EINVAL, ENODEV, EPERM, ESPIPE  and ETXTBSY
			// Appending was failed anyway, returns unexpected error
			err = errUnexpected
		}
		return err
	}

	defer func() {
		disk.Fdatasync(w) // Only interested in flushing the size_t not mtime/atime
		w.Close()
	}()

	bufp := s.pool.Get().(*[]byte)
	defer s.pool.Put(bufp)

	written, err := xioutil.CopyAligned(w, r, *bufp, fileSize)
	if err != nil {
		return err
	}

	if written < fileSize {
		return errLessData
	} else if written > fileSize {
		return errMoreData
	}

	return nil
}

func (s *xlStorage) WriteAll(ctx context.Context, volume string, path string, reader io.Reader) (err error) {
	atomic.AddInt32(&s.activeIOCount, 1)
	defer func() {
		atomic.AddInt32(&s.activeIOCount, -1)
	}()

	w, err := s.openFile(volume, path, os.O_CREATE|os.O_SYNC|os.O_WRONLY)
	if err != nil {
		return err
	}

	defer w.Close()

	bufp := s.pool.Get().(*[]byte)
	defer s.pool.Put(bufp)

	_, err = io.CopyBuffer(w, reader, *bufp)
	return err
}

// AppendFile - append a byte array at path, if file doesn't exist at
// path this call explicitly creates it.
func (s *xlStorage) AppendFile(ctx context.Context, volume string, path string, buf []byte) (err error) {
	atomic.AddInt32(&s.activeIOCount, 1)
	defer func() {
		atomic.AddInt32(&s.activeIOCount, -1)
	}()

	var w *os.File
	// Create file if not found. Not doing O_DIRECT here to avoid the code that does buffer aligned writes.
	// AppendFile() is only used by healing code to heal objects written in old format.
	w, err = s.openFile(volume, path, os.O_CREATE|os.O_SYNC|os.O_APPEND|os.O_WRONLY)
	if err != nil {
		return err
	}

	if _, err = w.Write(buf); err != nil {
		return err
	}

	return w.Close()
}

// CheckParts check if path has necessary parts available.
func (s *xlStorage) CheckParts(ctx context.Context, volume string, path string, fi FileInfo) error {
	atomic.AddInt32(&s.activeIOCount, 1)
	defer func() {
		atomic.AddInt32(&s.activeIOCount, -1)
	}()

	volumeDir, err := s.getVolDir(volume)
	if err != nil {
		return err
	}

	// Stat a volume entry.
	if _, err = os.Stat(volumeDir); err != nil {
		if os.IsNotExist(err) {
			return errVolumeNotFound
		}
		return err
	}

	for _, part := range fi.Parts {
		partPath := pathJoin(path, fi.DataDir, fmt.Sprintf("part.%d", part.Number))
		if fi.XLV1 {
			partPath = pathJoin(path, fmt.Sprintf("part.%d", part.Number))
		}
		filePath := pathJoin(volumeDir, partPath)
		if err = checkPathLength(filePath); err != nil {
			return err
		}
		st, err := os.Stat(filePath)
		if err != nil {
			return osErrToFileErr(err)
		}
		if st.Mode().IsDir() {
			return errFileNotFound
		}
		// Check if shard is truncated.
		if st.Size() < fi.Erasure.ShardFileSize(part.Size) {
			return errFileCorrupt
		}
	}

	return nil
}

// CheckFile check if path has necessary metadata.
func (s *xlStorage) CheckFile(ctx context.Context, volume string, path string) error {
	atomic.AddInt32(&s.activeIOCount, 1)
	defer func() {
		atomic.AddInt32(&s.activeIOCount, -1)
	}()

	volumeDir, err := s.getVolDir(volume)
	if err != nil {
		return err
	}

	// Stat a volume entry.
	_, err = os.Stat(volumeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return errVolumeNotFound
		}
		return err
	}

	filePath := pathJoin(volumeDir, path, xlStorageFormatFile)
	if err = checkPathLength(filePath); err != nil {
		return err
	}

	filePathOld := pathJoin(volumeDir, path, xlStorageFormatFileV1)
	if err = checkPathLength(filePathOld); err != nil {
		return err
	}

	st, err := os.Stat(filePath)
	if err != nil && !os.IsNotExist(err) {
		return osErrToFileErr(err)
	}
	if st == nil {
		st, err = os.Stat(filePathOld)
		if err != nil {
			return osErrToFileErr(err)
		}
	}

	// If its a directory its not a regular file.
	if st.Mode().IsDir() || st.Size() == 0 {
		return errFileNotFound
	}

	return nil
}

// deleteFile deletes a file or a directory if its empty unless recursive
// is set to true. If the target is successfully deleted, it will recursively
// move up the tree, deleting empty parent directories until it finds one
// with files in it. Returns nil for a non-empty directory even when
// recursive is set to false.
func deleteFile(basePath, deletePath string, recursive bool) error {
	if basePath == "" || deletePath == "" {
		return nil
	}
	isObjectDir := HasSuffix(deletePath, SlashSeparator)
	basePath = filepath.Clean(basePath)
	deletePath = filepath.Clean(deletePath)
	if !strings.HasPrefix(deletePath, basePath) || deletePath == basePath {
		return nil
	}

	var err error
	if recursive {
		err = os.RemoveAll(deletePath)
	} else {
		err = os.Remove(deletePath)
	}
	if err != nil {
		switch {
		case isSysErrNotEmpty(err):
			// if object is a directory, but if its not empty
			// return FileNotFound to indicate its an empty prefix.
			if isObjectDir {
				return errFileNotFound
			}
			// Ignore errors if the directory is not empty. The server relies on
			// this functionality, and sometimes uses recursion that should not
			// error on parent directories.
			return nil
		case os.IsNotExist(err):
			return errFileNotFound
		case os.IsPermission(err):
			return errFileAccessDenied
		case isSysErrIO(err):
			return errFaultyDisk
		default:
			return err
		}
	}

	deletePath = filepath.Dir(deletePath)

	// Delete parent directory obviously not recursively. Errors for
	// parent directories shouldn't trickle down.
	deleteFile(basePath, deletePath, false)

	return nil
}

// DeleteFile - delete a file at path.
func (s *xlStorage) Delete(ctx context.Context, volume string, path string, recursive bool) (err error) {
	atomic.AddInt32(&s.activeIOCount, 1)
	defer func() {
		atomic.AddInt32(&s.activeIOCount, -1)
	}()

	volumeDir, err := s.getVolDir(volume)
	if err != nil {
		return err
	}

	// Stat a volume entry.
	_, err = os.Stat(volumeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return errVolumeNotFound
		} else if os.IsPermission(err) {
			return errVolumeAccessDenied
		} else if isSysErrIO(err) {
			return errFaultyDisk
		}
		return err
	}

	// Following code is needed so that we retain SlashSeparator suffix if any in
	// path argument.
	filePath := pathJoin(volumeDir, path)
	if err = checkPathLength(filePath); err != nil {
		return err
	}

	// Delete file and delete parent directory as well if it's empty.
	return deleteFile(volumeDir, filePath, recursive)
}

func (s *xlStorage) DeleteFileBulk(volume string, paths []string) (errs []error, err error) {
	atomic.AddInt32(&s.activeIOCount, 1)
	defer func() {
		atomic.AddInt32(&s.activeIOCount, -1)
	}()

	volumeDir, err := s.getVolDir(volume)
	if err != nil {
		return nil, err
	}

	// Stat a volume entry.
	_, err = os.Stat(volumeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errVolumeNotFound
		} else if os.IsPermission(err) {
			return nil, errVolumeAccessDenied
		} else if isSysErrIO(err) {
			return nil, errFaultyDisk
		}
		return nil, err
	}

	errs = make([]error, len(paths))
	// Following code is needed so that we retain SlashSeparator
	// suffix if any in path argument.
	for idx, path := range paths {
		filePath := pathJoin(volumeDir, path)
		errs[idx] = checkPathLength(filePath)
		if errs[idx] != nil {
			continue
		}
		// Delete file and delete parent directory as well if its empty.
		errs[idx] = deleteFile(volumeDir, filePath, false)
	}
	return
}

// RenameData - rename source path to destination path atomically, metadata and data directory.
func (s *xlStorage) RenameData(ctx context.Context, srcVolume, srcPath, dataDir, dstVolume, dstPath string) (err error) {
	atomic.AddInt32(&s.activeIOCount, 1)
	defer func() {
		atomic.AddInt32(&s.activeIOCount, -1)
	}()

	srcVolumeDir, err := s.getVolDir(srcVolume)
	if err != nil {
		return err
	}

	dstVolumeDir, err := s.getVolDir(dstVolume)
	if err != nil {
		return err
	}

	// Stat a volume entry.
	_, err = os.Stat(srcVolumeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return errVolumeNotFound
		} else if isSysErrIO(err) {
			return errFaultyDisk
		}
		return err
	}
	_, err = os.Stat(dstVolumeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return errVolumeNotFound
		} else if isSysErrIO(err) {
			return errFaultyDisk
		}
		return err
	}

	srcFilePath := slashpath.Join(srcVolumeDir, pathJoin(srcPath, xlStorageFormatFile))
	dstFilePath := slashpath.Join(dstVolumeDir, pathJoin(dstPath, xlStorageFormatFile))

	var srcDataPath string
	var dstDataPath string
	if dataDir != "" {
		srcDataPath = retainSlash(pathJoin(srcVolumeDir, srcPath, dataDir))
		// make sure to always use path.Join here, do not use pathJoin as
		// it would additionally add `/` at the end and it comes in the
		// way of renameAll(), parentDir creation.
		dstDataPath = slashpath.Join(dstVolumeDir, dstPath, dataDir)
	}

	if err = checkPathLength(srcFilePath); err != nil {
		return err
	}

	if err = checkPathLength(dstFilePath); err != nil {
		return err
	}

	srcBuf, err := ioutil.ReadFile(srcFilePath)
	if err != nil {
		return osErrToFileErr(err)
	}

	fi, err := getFileInfo(srcBuf, dstVolume, dstPath, "")
	if err != nil {
		return err
	}

	dstBuf, err := ioutil.ReadFile(dstFilePath)
	if err != nil {
		if !os.IsNotExist(err) {
			return osErrToFileErr(err)
		}
		err = s.renameLegacyMetadata(dstVolume, dstPath)
		if err != nil && err != errFileNotFound {
			return err
		}
		if err == nil {
			dstBuf, err = ioutil.ReadFile(dstFilePath)
			if err != nil && !os.IsNotExist(err) {
				return osErrToFileErr(err)
			}
		}
	}

	var xlMeta xlMetaV2
	var legacyPreserved bool
	if len(dstBuf) > 0 {
		if isXL2V1Format(dstBuf) {
			if err = xlMeta.Load(dstBuf); err != nil {
				logger.LogIf(s.ctx, err)
				return errFileCorrupt
			}
		} else {
			// This code-path is to preserve the legacy data.
			xlMetaLegacy := &xlMetaV1Object{}
			var json = jsoniter.ConfigCompatibleWithStandardLibrary
			if err := json.Unmarshal(dstBuf, xlMetaLegacy); err != nil {
				logger.LogIf(s.ctx, err)
				return errFileCorrupt
			}
			if err = xlMeta.AddLegacy(xlMetaLegacy); err != nil {
				logger.LogIf(s.ctx, err)
				return errFileCorrupt
			}
			legacyPreserved = true
		}
	} else {
		// It is possible that some drives may not have `xl.meta` file
		// in such scenarios verify if atleast `part.1` files exist
		// to verify for legacy version.
		currentDataPath := pathJoin(dstVolumeDir, dstPath)
		entries, err := readDirN(currentDataPath, 1)
		if err != nil && err != errFileNotFound {
			return osErrToFileErr(err)
		}
		for _, entry := range entries {
			if entry == xlStorageFormatFile || strings.HasSuffix(entry, slashSeparator) {
				continue
			}
			if strings.HasPrefix(entry, "part.") {
				legacyPreserved = true
				break
			}
		}
	}

	if legacyPreserved {
		// Preserve all the legacy data, could be slow, but at max there can be 10,000 parts.
		currentDataPath := pathJoin(dstVolumeDir, dstPath)
		entries, err := readDir(currentDataPath)
		if err != nil {
			return osErrToFileErr(err)
		}

		legacyDataPath := pathJoin(dstVolumeDir, dstPath, legacyDataDir)
		// legacy data dir means its old content, honor system umask.
		if err = os.MkdirAll(legacyDataPath, 0777); err != nil {
			return osErrToFileErr(err)
		}

		if s.globalSync {
			// Sync all the previous directory operations.
			globalSync()
		}

		for _, entry := range entries {
			// Skip xl.meta renames further, also ignore any directories such as `legacyDataDir`
			if entry == xlStorageFormatFile || strings.HasSuffix(entry, slashSeparator) {
				continue
			}

			if err = os.Rename(pathJoin(currentDataPath, entry), pathJoin(legacyDataPath, entry)); err != nil {
				return osErrToFileErr(err)
			}
		}

		// Sync all the metadata operations once renames are done.
		if s.globalSync {
			globalSync()
		}
	}

	var oldDstDataPath string
	if fi.VersionID == "" {
		// return the latest "null" versionId info
		ofi, err := xlMeta.ToFileInfo(dstVolume, dstPath, nullVersionID)
		if err == nil && !ofi.Deleted {
			// Purge the destination path as we are not preserving anything
			// versioned object was not requested.
			oldDstDataPath = pathJoin(dstVolumeDir, dstPath, ofi.DataDir)
		}
	}

	if err = xlMeta.AddVersion(fi); err != nil {
		return err
	}

	dstBuf, err = xlMeta.MarshalMsg(append(xlHeader[:], xlVersionV1[:]...))
	if err != nil {
		return errFileCorrupt
	}

	if err = s.WriteAll(ctx, srcVolume, pathJoin(srcPath, xlStorageFormatFile), bytes.NewReader(dstBuf)); err != nil {
		return err
	}

	// Commit data
	if srcDataPath != "" {
		removeAll(oldDstDataPath)
		removeAll(dstDataPath)
		if err = renameAll(srcDataPath, dstDataPath); err != nil {
			return osErrToFileErr(err)
		}
	}

	// Commit meta-file
	if err = renameAll(srcFilePath, dstFilePath); err != nil {
		return osErrToFileErr(err)
	}

	// Remove parent dir of the source file if empty
	if parentDir := slashpath.Dir(srcFilePath); isDirEmpty(parentDir) {
		deleteFile(srcVolumeDir, parentDir, false)
	}

	if srcDataPath != "" {
		if parentDir := slashpath.Dir(srcDataPath); isDirEmpty(parentDir) {
			deleteFile(srcVolumeDir, parentDir, false)
		}
	}

	return nil
}

// RenameFile - rename source path to destination path atomically.
func (s *xlStorage) RenameFile(ctx context.Context, srcVolume, srcPath, dstVolume, dstPath string) (err error) {
	atomic.AddInt32(&s.activeIOCount, 1)
	defer func() {
		atomic.AddInt32(&s.activeIOCount, -1)
	}()

	srcVolumeDir, err := s.getVolDir(srcVolume)
	if err != nil {
		return err
	}
	dstVolumeDir, err := s.getVolDir(dstVolume)
	if err != nil {
		return err
	}
	// Stat a volume entry.
	_, err = os.Stat(srcVolumeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return errVolumeNotFound
		} else if isSysErrIO(err) {
			return errFaultyDisk
		}
		return err
	}
	_, err = os.Stat(dstVolumeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return errVolumeNotFound
		} else if isSysErrIO(err) {
			return errFaultyDisk
		}
		return err
	}

	srcIsDir := HasSuffix(srcPath, SlashSeparator)
	dstIsDir := HasSuffix(dstPath, SlashSeparator)
	// Either src and dst have to be directories or files, else return error.
	if !(srcIsDir && dstIsDir || !srcIsDir && !dstIsDir) {
		return errFileAccessDenied
	}
	srcFilePath := slashpath.Join(srcVolumeDir, srcPath)
	if err = checkPathLength(srcFilePath); err != nil {
		return err
	}
	dstFilePath := slashpath.Join(dstVolumeDir, dstPath)
	if err = checkPathLength(dstFilePath); err != nil {
		return err
	}
	if srcIsDir {
		// If source is a directory, we expect the destination to be non-existent but we
		// we still need to allow overwriting an empty directory since it represents
		// an object empty directory.
		_, err = os.Stat(dstFilePath)
		if isSysErrIO(err) {
			return errFaultyDisk
		}
		if err == nil && !isDirEmpty(dstFilePath) {
			return errFileAccessDenied
		}
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		// Empty destination remove it before rename.
		if isDirEmpty(dstFilePath) {
			if err = os.Remove(dstFilePath); err != nil {
				if isSysErrNotEmpty(err) {
					return errFileAccessDenied
				}
				return err
			}
		}
	}

	if err = renameAll(srcFilePath, dstFilePath); err != nil {
		return osErrToFileErr(err)
	}

	// Remove parent dir of the source file if empty
	if parentDir := slashpath.Dir(srcFilePath); isDirEmpty(parentDir) {
		deleteFile(srcVolumeDir, parentDir, false)
	}

	return nil
}

func (s *xlStorage) bitrotVerify(partPath string, partSize int64, algo BitrotAlgorithm, sum []byte, shardSize int64) error {
	// Open the file for reading.
	file, err := os.Open(partPath)
	if err != nil {
		return osErrToFileErr(err)
	}

	// Close the file descriptor.
	defer file.Close()

	if algo != HighwayHash256S {
		bufp := s.pool.Get().(*[]byte)
		defer s.pool.Put(bufp)

		h := algo.New()
		if _, err = io.CopyBuffer(h, file, *bufp); err != nil {
			// Premature failure in reading the object,file is corrupt.
			return errFileCorrupt
		}
		if !bytes.Equal(h.Sum(nil), sum) {
			return errFileCorrupt
		}
		return nil
	}

	buf := make([]byte, shardSize)
	h := algo.New()
	hashBuf := make([]byte, h.Size())
	fi, err := file.Stat()
	if err != nil {
		// Unable to stat on the file, return an expected error
		// for healing code to fix this file.
		return err
	}

	size := fi.Size()

	// Calculate the size of the bitrot file and compare
	// it with the actual file size.
	if size != bitrotShardFileSize(partSize, shardSize, algo) {
		return errFileCorrupt
	}

	var n int
	for {
		if size == 0 {
			return nil
		}
		h.Reset()
		n, err = file.Read(hashBuf)
		if err != nil {
			// Read's failed for object with right size, file is corrupt.
			return err
		}
		size -= int64(n)
		if size < int64(len(buf)) {
			buf = buf[:size]
		}
		n, err = file.Read(buf)
		if err != nil {
			// Read's failed for object with right size, at different offsets.
			return err
		}
		size -= int64(n)
		h.Write(buf)
		if !bytes.Equal(h.Sum(nil), hashBuf) {
			return errFileCorrupt
		}
	}
}

func (s *xlStorage) VerifyFile(ctx context.Context, volume, path string, fi FileInfo) (err error) {
	atomic.AddInt32(&s.activeIOCount, 1)
	defer func() {
		atomic.AddInt32(&s.activeIOCount, -1)
	}()

	volumeDir, err := s.getVolDir(volume)
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
		} else if os.IsPermission(err) {
			return errVolumeAccessDenied
		}
		return err
	}

	erasure := fi.Erasure
	for _, part := range fi.Parts {
		checksumInfo := erasure.GetChecksumInfo(part.Number)
		partPath := pathJoin(volumeDir, path, fi.DataDir, fmt.Sprintf("part.%d", part.Number))
		if fi.XLV1 {
			partPath = pathJoin(volumeDir, path, fmt.Sprintf("part.%d", part.Number))
		}
		if err := s.bitrotVerify(partPath,
			erasure.ShardFileSize(part.Size),
			checksumInfo.Algorithm,
			checksumInfo.Hash, erasure.ShardSize()); err != nil {
			if !IsErr(err, []error{
				errFileNotFound,
				errVolumeNotFound,
				errFileCorrupt,
			}...) {
				logger.GetReqInfo(s.ctx).AppendTags("disk", s.String())
				logger.LogIf(s.ctx, err)
			}
			return err
		}
	}

	return nil
}
