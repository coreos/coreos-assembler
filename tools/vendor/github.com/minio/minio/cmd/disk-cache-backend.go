// Copyright (c) 2015-2021 MinIO, Inc.
//
// This file is part of MinIO Object Storage stack
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package cmd

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/djherbis/atime"
	"github.com/minio/minio/internal/config/cache"
	"github.com/minio/minio/internal/crypto"
	"github.com/minio/minio/internal/disk"
	"github.com/minio/minio/internal/fips"
	xhttp "github.com/minio/minio/internal/http"
	xioutil "github.com/minio/minio/internal/ioutil"
	"github.com/minio/minio/internal/kms"
	"github.com/minio/minio/internal/logger"
	"github.com/minio/sio"
)

const (
	// cache.json object metadata for cached objects.
	cacheMetaJSONFile = "cache.json"
	cacheDataFile     = "part.1"
	cacheMetaVersion  = "1.0.0"
	cacheExpiryDays   = 90 * time.Hour * 24 // defaults to 90 days
	// SSECacheEncrypted is the metadata key indicating that the object
	// is a cache entry encrypted with cache KMS master key in globalCacheKMS.
	SSECacheEncrypted = "X-Minio-Internal-Encrypted-Cache"
)

// CacheChecksumInfoV1 - carries checksums of individual blocks on disk.
type CacheChecksumInfoV1 struct {
	Algorithm string `json:"algorithm"`
	Blocksize int64  `json:"blocksize"`
}

// Represents the cache metadata struct
type cacheMeta struct {
	Version string   `json:"version"`
	Stat    StatInfo `json:"stat"` // Stat of the current object `cache.json`.

	// checksums of blocks on disk.
	Checksum CacheChecksumInfoV1 `json:"checksum,omitempty"`
	// Metadata map for current object.
	Meta map[string]string `json:"meta,omitempty"`
	// Ranges maps cached range to associated filename.
	Ranges map[string]string `json:"ranges,omitempty"`
	// Hits is a counter on the number of times this object has been accessed so far.
	Hits   int    `json:"hits,omitempty"`
	Bucket string `json:"bucket,omitempty"`
	Object string `json:"object,omitempty"`
}

// RangeInfo has the range, file and range length information for a cached range.
type RangeInfo struct {
	Range string
	File  string
	Size  int64
}

// Empty returns true if this is an empty struct
func (r *RangeInfo) Empty() bool {
	return r.Range == "" && r.File == "" && r.Size == 0
}

func (m *cacheMeta) ToObjectInfo(bucket, object string) (o ObjectInfo) {
	if len(m.Meta) == 0 {
		m.Meta = make(map[string]string)
		m.Stat.ModTime = timeSentinel
	}

	o = ObjectInfo{
		Bucket:            bucket,
		Name:              object,
		CacheStatus:       CacheHit,
		CacheLookupStatus: CacheHit,
	}

	// We set file info only if its valid.
	o.Size = m.Stat.Size
	o.ETag = extractETag(m.Meta)
	o.ContentType = m.Meta["content-type"]
	o.ContentEncoding = m.Meta["content-encoding"]
	if storageClass, ok := m.Meta[xhttp.AmzStorageClass]; ok {
		o.StorageClass = storageClass
	} else {
		o.StorageClass = globalMinioDefaultStorageClass
	}
	var (
		t time.Time
		e error
	)
	if exp, ok := m.Meta["expires"]; ok {
		if t, e = time.Parse(http.TimeFormat, exp); e == nil {
			o.Expires = t.UTC()
		}
	}
	if mtime, ok := m.Meta["last-modified"]; ok {
		if t, e = time.Parse(http.TimeFormat, mtime); e == nil {
			o.ModTime = t.UTC()
		}
	}

	// etag/md5Sum has already been extracted. We need to
	// remove to avoid it from appearing as part of user-defined metadata
	o.UserDefined = cleanMetadata(m.Meta)
	return o
}

// represents disk cache struct
type diskCache struct {
	// is set to 0 if drive is offline
	online       uint32 // ref: https://golang.org/pkg/sync/atomic/#pkg-note-BUG
	purgeRunning int32

	triggerGC        chan struct{}
	dir              string         // caching directory
	stats            CacheDiskStats // disk cache stats for prometheus
	quotaPct         int            // max usage in %
	pool             sync.Pool
	after            int // minimum accesses before an object is cached.
	lowWatermark     int
	highWatermark    int
	enableRange      bool
	commitWriteback  bool
	retryWritebackCh chan ObjectInfo
	// nsMutex namespace lock
	nsMutex *nsLockMap
	// Object functions pointing to the corresponding functions of backend implementation.
	NewNSLockFn func(cachePath string) RWLocker
}

// Inits the disk cache dir if it is not initialized already.
func newDiskCache(ctx context.Context, dir string, config cache.Config) (*diskCache, error) {
	quotaPct := config.MaxUse
	if quotaPct == 0 {
		quotaPct = config.Quota
	}

	if err := os.MkdirAll(dir, 0777); err != nil {
		return nil, fmt.Errorf("Unable to initialize '%s' dir, %w", dir, err)
	}
	cache := diskCache{
		dir:              dir,
		triggerGC:        make(chan struct{}, 1),
		stats:            CacheDiskStats{Dir: dir},
		quotaPct:         quotaPct,
		after:            config.After,
		lowWatermark:     config.WatermarkLow,
		highWatermark:    config.WatermarkHigh,
		enableRange:      config.Range,
		commitWriteback:  config.CommitWriteback,
		retryWritebackCh: make(chan ObjectInfo, 10000),
		online:           1,
		pool: sync.Pool{
			New: func() interface{} {
				b := disk.AlignedBlock(int(cacheBlkSize))
				return &b
			},
		},
		nsMutex: newNSLock(false),
	}
	go cache.purgeWait(ctx)
	if cache.commitWriteback {
		go cache.scanCacheWritebackFailures(ctx)
	}
	cache.diskSpaceAvailable(0) // update if cache usage is already high.
	cache.NewNSLockFn = func(cachePath string) RWLocker {
		return cache.nsMutex.NewNSLock(nil, cachePath, "")
	}
	return &cache, nil
}

// diskUsageLow() returns true if disk usage falls below the low watermark w.r.t configured cache quota.
// Ex. for a 100GB disk, if quota is configured as 70%  and watermark_low = 80% and
// watermark_high = 90% then garbage collection starts when 63% of disk is used and
// stops when disk usage drops to 56%
func (c *diskCache) diskUsageLow() bool {
	gcStopPct := c.quotaPct * c.lowWatermark / 100
	di, err := disk.GetInfo(c.dir)
	if err != nil {
		reqInfo := (&logger.ReqInfo{}).AppendTags("cachePath", c.dir)
		ctx := logger.SetReqInfo(GlobalContext, reqInfo)
		logger.LogIf(ctx, err)
		return false
	}
	usedPercent := float64(di.Used) * 100 / float64(di.Total)
	low := int(usedPercent) < gcStopPct
	atomic.StoreUint64(&c.stats.UsagePercent, uint64(usedPercent))
	if low {
		atomic.StoreInt32(&c.stats.UsageState, 0)
	}
	return low
}

// Returns if the disk usage reaches  or exceeds configured cache quota when size is added.
// If current usage without size exceeds high watermark a GC is automatically queued.
func (c *diskCache) diskSpaceAvailable(size int64) bool {
	gcTriggerPct := c.quotaPct * c.highWatermark / 100
	di, err := disk.GetInfo(c.dir)
	if err != nil {
		reqInfo := (&logger.ReqInfo{}).AppendTags("cachePath", c.dir)
		ctx := logger.SetReqInfo(GlobalContext, reqInfo)
		logger.LogIf(ctx, err)
		return false
	}
	if di.Total == 0 {
		logger.Info("diskCache: Received 0 total disk size")
		return false
	}
	usedPercent := float64(di.Used) * 100 / float64(di.Total)
	if usedPercent >= float64(gcTriggerPct) {
		atomic.StoreInt32(&c.stats.UsageState, 1)
		c.queueGC()
	}
	atomic.StoreUint64(&c.stats.UsagePercent, uint64(usedPercent))

	// Recalculate percentage with provided size added.
	usedPercent = float64(di.Used+uint64(size)) * 100 / float64(di.Total)

	return usedPercent < float64(c.quotaPct)
}

// queueGC will queue a GC.
// Calling this function is always non-blocking.
func (c *diskCache) queueGC() {
	select {
	case c.triggerGC <- struct{}{}:
	default:
	}
}

// toClear returns how many bytes should be cleared to reach the low watermark quota.
// returns 0 if below quota.
func (c *diskCache) toClear() uint64 {
	di, err := disk.GetInfo(c.dir)
	if err != nil {
		reqInfo := (&logger.ReqInfo{}).AppendTags("cachePath", c.dir)
		ctx := logger.SetReqInfo(GlobalContext, reqInfo)
		logger.LogIf(ctx, err)
		return 0
	}
	return bytesToClear(int64(di.Total), int64(di.Free), uint64(c.quotaPct), uint64(c.lowWatermark), uint64(c.highWatermark))
}

func (c *diskCache) purgeWait(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
		case <-c.triggerGC: // wait here until someone triggers.
			c.purge(ctx)
		}
	}
}

// Purge cache entries that were not accessed.
func (c *diskCache) purge(ctx context.Context) {
	if atomic.LoadInt32(&c.purgeRunning) == 1 || c.diskUsageLow() {
		return
	}

	toFree := c.toClear()
	if toFree == 0 {
		return
	}

	atomic.StoreInt32(&c.purgeRunning, 1) // do not run concurrent purge()
	defer atomic.StoreInt32(&c.purgeRunning, 0)

	// expiry for cleaning up old cache.json files that
	// need to be cleaned up.
	expiry := UTCNow().Add(-cacheExpiryDays)
	// defaulting max hits count to 100
	// ignore error we know what value we are passing.
	scorer, _ := newFileScorer(toFree, time.Now().Unix(), 100)

	// this function returns FileInfo for cached range files and cache data file.
	fiStatFn := func(ranges map[string]string, dataFile, pathPrefix string) map[string]os.FileInfo {
		fm := make(map[string]os.FileInfo)
		fname := pathJoin(pathPrefix, dataFile)
		if fi, err := os.Stat(fname); err == nil {
			fm[fname] = fi
		}

		for _, rngFile := range ranges {
			fname = pathJoin(pathPrefix, rngFile)
			if fi, err := os.Stat(fname); err == nil {
				fm[fname] = fi
			}
		}
		return fm
	}

	filterFn := func(name string, typ os.FileMode) error {
		if name == minioMetaBucket {
			// Proceed to next file.
			return nil
		}

		cacheDir := pathJoin(c.dir, name)
		meta, _, numHits, err := c.statCachedMeta(ctx, cacheDir)
		if err != nil {
			// delete any partially filled cache entry left behind.
			removeAll(cacheDir)
			// Proceed to next file.
			return nil
		}

		// stat all cached file ranges and cacheDataFile.
		cachedFiles := fiStatFn(meta.Ranges, cacheDataFile, pathJoin(c.dir, name))
		objInfo := meta.ToObjectInfo("", "")
		// prevent gc from clearing un-synced commits. This metadata is present when
		// cache writeback commit setting is enabled.
		status, ok := objInfo.UserDefined[writeBackStatusHeader]
		if ok && status != CommitComplete.String() {
			return nil
		}
		cc := cacheControlOpts(objInfo)
		for fname, fi := range cachedFiles {
			if cc != nil {
				if cc.isStale(objInfo.ModTime) {
					if err = removeAll(fname); err != nil {
						logger.LogIf(ctx, err)
					}
					scorer.adjustSaveBytes(-fi.Size())

					// break early if sufficient disk space reclaimed.
					if c.diskUsageLow() {
						// if we found disk usage is already low, we return nil filtering is complete.
						return errDoneForNow
					}
				}
				continue
			}
			scorer.addFile(fname, atime.Get(fi), fi.Size(), numHits)
		}
		// clean up stale cache.json files for objects that never got cached but access count was maintained in cache.json
		fi, err := os.Stat(pathJoin(cacheDir, cacheMetaJSONFile))
		if err != nil || (fi.ModTime().Before(expiry) && len(cachedFiles) == 0) {
			removeAll(cacheDir)
			scorer.adjustSaveBytes(-fi.Size())
			// Proceed to next file.
			return nil
		}

		// if we found disk usage is already low, we return nil filtering is complete.
		if c.diskUsageLow() {
			return errDoneForNow
		}

		// Proceed to next file.
		return nil
	}

	if err := readDirFn(c.dir, filterFn); err != nil {
		logger.LogIf(ctx, err)
		return
	}

	scorer.purgeFunc(func(qfile queuedFile) {
		fileName := qfile.name
		removeAll(fileName)
		slashIdx := strings.LastIndex(fileName, SlashSeparator)
		if slashIdx >= 0 {
			fileNamePrefix := fileName[0:slashIdx]
			fname := fileName[slashIdx+1:]
			if fname == cacheDataFile {
				removeAll(fileNamePrefix)
			}
		}
	})

	scorer.reset()
}

// sets cache drive status
func (c *diskCache) setOffline() {
	atomic.StoreUint32(&c.online, 0)
}

// returns true if cache drive is online
func (c *diskCache) IsOnline() bool {
	return atomic.LoadUint32(&c.online) != 0
}

// Stat returns ObjectInfo from disk cache
func (c *diskCache) Stat(ctx context.Context, bucket, object string) (oi ObjectInfo, numHits int, err error) {
	var partial bool
	var meta *cacheMeta

	cacheObjPath := getCacheSHADir(c.dir, bucket, object)
	// Stat the file to get file size.
	meta, partial, numHits, err = c.statCachedMeta(ctx, cacheObjPath)
	if err != nil {
		return
	}
	if partial {
		return oi, numHits, errFileNotFound
	}
	oi = meta.ToObjectInfo("", "")
	oi.Bucket = bucket
	oi.Name = object

	if err = decryptCacheObjectETag(&oi); err != nil {
		return
	}
	return
}

// statCachedMeta returns metadata from cache - including ranges cached, partial to indicate
// if partial object is cached.
func (c *diskCache) statCachedMeta(ctx context.Context, cacheObjPath string) (meta *cacheMeta, partial bool, numHits int, err error) {
	cLock := c.NewNSLockFn(cacheObjPath)
	lkctx, err := cLock.GetRLock(ctx, globalOperationTimeout)
	if err != nil {
		return
	}
	ctx = lkctx.Context()
	defer cLock.RUnlock(lkctx.Cancel)
	return c.statCache(ctx, cacheObjPath)
}

// statRange returns ObjectInfo and RangeInfo from disk cache
func (c *diskCache) statRange(ctx context.Context, bucket, object string, rs *HTTPRangeSpec) (oi ObjectInfo, rngInfo RangeInfo, numHits int, err error) {
	// Stat the file to get file size.
	cacheObjPath := getCacheSHADir(c.dir, bucket, object)
	var meta *cacheMeta
	var partial bool

	meta, partial, numHits, err = c.statCachedMeta(ctx, cacheObjPath)
	if err != nil {
		return
	}

	oi = meta.ToObjectInfo("", "")
	oi.Bucket = bucket
	oi.Name = object
	if !partial {
		err = decryptCacheObjectETag(&oi)
		return
	}

	actualSize := uint64(meta.Stat.Size)
	var length int64
	_, length, err = rs.GetOffsetLength(int64(actualSize))
	if err != nil {
		return
	}

	actualRngSize := uint64(length)
	if globalCacheKMS != nil {
		actualRngSize, _ = sio.EncryptedSize(uint64(length))
	}

	rng := rs.String(int64(actualSize))
	rngFile, ok := meta.Ranges[rng]
	if !ok {
		return oi, rngInfo, numHits, ObjectNotFound{Bucket: bucket, Object: object}
	}
	if _, err = os.Stat(pathJoin(cacheObjPath, rngFile)); err != nil {
		return oi, rngInfo, numHits, ObjectNotFound{Bucket: bucket, Object: object}
	}
	rngInfo = RangeInfo{Range: rng, File: rngFile, Size: int64(actualRngSize)}

	err = decryptCacheObjectETag(&oi)
	return
}

// statCache is a convenience function for purge() to get ObjectInfo for cached object
func (c *diskCache) statCache(ctx context.Context, cacheObjPath string) (meta *cacheMeta, partial bool, numHits int, err error) {
	// Stat the file to get file size.
	metaPath := pathJoin(cacheObjPath, cacheMetaJSONFile)
	f, err := os.Open(metaPath)
	if err != nil {
		return meta, partial, 0, err
	}
	defer f.Close()
	meta = &cacheMeta{Version: cacheMetaVersion}
	if err := jsonLoad(f, meta); err != nil {
		return meta, partial, 0, err
	}
	// get metadata of part.1 if full file has been cached.
	partial = true
	if _, err := os.Stat(pathJoin(cacheObjPath, cacheDataFile)); err == nil {
		partial = false
	}
	return meta, partial, meta.Hits, nil
}

// saves object metadata to disk cache
// incHitsOnly is true if metadata update is incrementing only the hit counter
func (c *diskCache) SaveMetadata(ctx context.Context, bucket, object string, meta map[string]string, actualSize int64, rs *HTTPRangeSpec, rsFileName string, incHitsOnly bool) error {
	cachedPath := getCacheSHADir(c.dir, bucket, object)
	cLock := c.NewNSLockFn(cachedPath)
	lkctx, err := cLock.GetLock(ctx, globalOperationTimeout)
	if err != nil {
		return err
	}
	ctx = lkctx.Context()
	defer cLock.Unlock(lkctx.Cancel)
	return c.saveMetadata(ctx, bucket, object, meta, actualSize, rs, rsFileName, incHitsOnly)
}

// saves object metadata to disk cache
// incHitsOnly is true if metadata update is incrementing only the hit counter
func (c *diskCache) saveMetadata(ctx context.Context, bucket, object string, meta map[string]string, actualSize int64, rs *HTTPRangeSpec, rsFileName string, incHitsOnly bool) error {
	cachedPath := getCacheSHADir(c.dir, bucket, object)
	metaPath := pathJoin(cachedPath, cacheMetaJSONFile)
	// Create cache directory if needed
	if err := os.MkdirAll(cachedPath, 0777); err != nil {
		return err
	}
	f, err := os.OpenFile(metaPath, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		return err
	}
	defer f.Close()

	m := &cacheMeta{
		Version: cacheMetaVersion,
		Bucket:  bucket,
		Object:  object,
	}
	if err := jsonLoad(f, m); err != nil && err != io.EOF {
		return err
	}
	// increment hits
	if rs != nil {
		// rsFileName gets set by putRange. Check for blank values here
		// coming from other code paths that set rs only (eg initial creation or hit increment).
		if rsFileName != "" {
			if m.Ranges == nil {
				m.Ranges = make(map[string]string)
			}
			m.Ranges[rs.String(actualSize)] = rsFileName
		}
	}
	if rs == nil && !incHitsOnly {
		// this is necessary cleanup of range files if entire object is cached.
		if _, err := os.Stat(pathJoin(cachedPath, cacheDataFile)); err == nil {
			for _, f := range m.Ranges {
				removeAll(pathJoin(cachedPath, f))
			}
			m.Ranges = nil
		}
	}
	m.Stat.Size = actualSize
	if !incHitsOnly {
		// reset meta
		m.Meta = meta
	} else {
		if m.Meta == nil {
			m.Meta = make(map[string]string)
		}
		if etag, ok := meta["etag"]; ok {
			m.Meta["etag"] = etag
		}
	}
	m.Hits++

	m.Checksum = CacheChecksumInfoV1{Algorithm: HighwayHash256S.String(), Blocksize: cacheBlkSize}
	return jsonSave(f, m)
}

func getCacheSHADir(dir, bucket, object string) string {
	return pathJoin(dir, getSHA256Hash([]byte(pathJoin(bucket, object))))
}

// Cache data to disk with bitrot checksum added for each block of 1MB
func (c *diskCache) bitrotWriteToCache(cachePath, fileName string, reader io.Reader, size uint64) (int64, string, error) {
	if err := os.MkdirAll(cachePath, 0777); err != nil {
		return 0, "", err
	}
	filePath := pathJoin(cachePath, fileName)

	if filePath == "" || reader == nil {
		return 0, "", errInvalidArgument
	}

	if err := checkPathLength(filePath); err != nil {
		return 0, "", err
	}
	f, err := os.Create(filePath)
	if err != nil {
		return 0, "", osErrToFileErr(err)
	}
	defer f.Close()

	var bytesWritten int64

	h := HighwayHash256S.New()

	bufp := c.pool.Get().(*[]byte)
	defer c.pool.Put(bufp)
	md5Hash := md5.New()
	var n, n2 int
	for {
		n, err = io.ReadFull(reader, *bufp)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return 0, "", err
		}
		eof := err == io.EOF || err == io.ErrUnexpectedEOF
		if n == 0 && size != 0 {
			// Reached EOF, nothing more to be done.
			break
		}
		h.Reset()
		if _, err = h.Write((*bufp)[:n]); err != nil {
			return 0, "", err
		}
		hashBytes := h.Sum(nil)
		// compute md5Hash of original data stream if writeback commit to cache
		if c.commitWriteback {
			if _, err = md5Hash.Write((*bufp)[:n]); err != nil {
				return 0, "", err
			}
		}
		if _, err = f.Write(hashBytes); err != nil {
			return 0, "", err
		}
		if n2, err = f.Write((*bufp)[:n]); err != nil {
			return 0, "", err
		}
		bytesWritten += int64(n2)
		if eof {
			break
		}
	}

	return bytesWritten, base64.StdEncoding.EncodeToString(md5Hash.Sum(nil)), nil
}

func newCacheEncryptReader(content io.Reader, bucket, object string, metadata map[string]string) (r io.Reader, err error) {
	objectEncryptionKey, err := newCacheEncryptMetadata(bucket, object, metadata)
	if err != nil {
		return nil, err
	}

	reader, err := sio.EncryptReader(content, sio.Config{Key: objectEncryptionKey[:], MinVersion: sio.Version20, CipherSuites: fips.CipherSuitesDARE()})
	if err != nil {
		return nil, crypto.ErrInvalidCustomerKey
	}
	return reader, nil
}
func newCacheEncryptMetadata(bucket, object string, metadata map[string]string) ([]byte, error) {
	var sealedKey crypto.SealedKey
	if globalCacheKMS == nil {
		return nil, errKMSNotConfigured
	}
	key, err := globalCacheKMS.GenerateKey("", kms.Context{bucket: pathJoin(bucket, object)})
	if err != nil {
		return nil, err
	}

	objectKey := crypto.GenerateKey(key.Plaintext, rand.Reader)
	sealedKey = objectKey.Seal(key.Plaintext, crypto.GenerateIV(rand.Reader), crypto.S3.String(), bucket, object)
	crypto.S3.CreateMetadata(metadata, key.KeyID, key.Ciphertext, sealedKey)

	if etag, ok := metadata["etag"]; ok {
		metadata["etag"] = hex.EncodeToString(objectKey.SealETag([]byte(etag)))
	}
	metadata[SSECacheEncrypted] = ""
	return objectKey[:], nil
}

// Caches the object to disk
func (c *diskCache) Put(ctx context.Context, bucket, object string, data io.Reader, size int64, rs *HTTPRangeSpec, opts ObjectOptions, incHitsOnly bool) (oi ObjectInfo, err error) {
	if !c.diskSpaceAvailable(size) {
		io.Copy(ioutil.Discard, data)
		return oi, errDiskFull
	}
	cachePath := getCacheSHADir(c.dir, bucket, object)
	cLock := c.NewNSLockFn(cachePath)
	lkctx, err := cLock.GetLock(ctx, globalOperationTimeout)
	if err != nil {
		return oi, err
	}
	ctx = lkctx.Context()
	defer cLock.Unlock(lkctx.Cancel)

	meta, _, numHits, err := c.statCache(ctx, cachePath)
	// Case where object not yet cached
	if osIsNotExist(err) && c.after >= 1 {
		return oi, c.saveMetadata(ctx, bucket, object, opts.UserDefined, size, nil, "", false)
	}
	// Case where object already has a cache metadata entry but not yet cached
	if err == nil && numHits < c.after {
		cETag := extractETag(meta.Meta)
		bETag := extractETag(opts.UserDefined)
		if cETag == bETag {
			return oi, c.saveMetadata(ctx, bucket, object, opts.UserDefined, size, nil, "", false)
		}
		incHitsOnly = true
	}

	if rs != nil {
		return oi, c.putRange(ctx, bucket, object, data, size, rs, opts)
	}
	if !c.diskSpaceAvailable(size) {
		return oi, errDiskFull
	}
	if err := os.MkdirAll(cachePath, 0777); err != nil {
		return oi, err
	}
	var metadata = cloneMSS(opts.UserDefined)
	var reader = data
	var actualSize = uint64(size)
	if globalCacheKMS != nil {
		reader, err = newCacheEncryptReader(data, bucket, object, metadata)
		if err != nil {
			return oi, err
		}
		actualSize, _ = sio.EncryptedSize(uint64(size))
	}
	n, md5sum, err := c.bitrotWriteToCache(cachePath, cacheDataFile, reader, actualSize)
	if IsErr(err, baseErrs...) {
		// take the cache drive offline
		c.setOffline()
	}
	if err != nil {
		removeAll(cachePath)
		return oi, err
	}

	if actualSize != uint64(n) {
		removeAll(cachePath)
		return oi, IncompleteBody{Bucket: bucket, Object: object}
	}
	if c.commitWriteback {
		metadata["content-md5"] = md5sum
		if md5bytes, err := base64.StdEncoding.DecodeString(md5sum); err == nil {
			metadata["etag"] = hex.EncodeToString(md5bytes)
		}
		metadata[writeBackStatusHeader] = CommitPending.String()
	}
	return ObjectInfo{
			Bucket:      bucket,
			Name:        object,
			ETag:        metadata["etag"],
			Size:        n,
			UserDefined: metadata,
		},
		c.saveMetadata(ctx, bucket, object, metadata, n, nil, "", incHitsOnly)
}

// Caches the range to disk
func (c *diskCache) putRange(ctx context.Context, bucket, object string, data io.Reader, size int64, rs *HTTPRangeSpec, opts ObjectOptions) error {
	rlen, err := rs.GetLength(size)
	if err != nil {
		return err
	}
	if !c.diskSpaceAvailable(rlen) {
		return errDiskFull
	}
	cachePath := getCacheSHADir(c.dir, bucket, object)
	if err := os.MkdirAll(cachePath, 0777); err != nil {
		return err
	}
	var metadata = cloneMSS(opts.UserDefined)
	var reader = data
	var actualSize = uint64(rlen)
	// objSize is the actual size of object (with encryption overhead if any)
	var objSize = uint64(size)
	if globalCacheKMS != nil {
		reader, err = newCacheEncryptReader(data, bucket, object, metadata)
		if err != nil {
			return err
		}
		actualSize, _ = sio.EncryptedSize(uint64(rlen))
		objSize, _ = sio.EncryptedSize(uint64(size))

	}
	cacheFile := MustGetUUID()
	n, _, err := c.bitrotWriteToCache(cachePath, cacheFile, reader, actualSize)
	if IsErr(err, baseErrs...) {
		// take the cache drive offline
		c.setOffline()
	}
	if err != nil {
		removeAll(cachePath)
		return err
	}
	if actualSize != uint64(n) {
		removeAll(cachePath)
		return IncompleteBody{Bucket: bucket, Object: object}
	}
	return c.saveMetadata(ctx, bucket, object, metadata, int64(objSize), rs, cacheFile, false)
}

// checks streaming bitrot checksum of cached object before returning data
func (c *diskCache) bitrotReadFromCache(ctx context.Context, filePath string, offset, length int64, writer io.Writer) error {
	h := HighwayHash256S.New()

	checksumHash := make([]byte, h.Size())

	startBlock := offset / cacheBlkSize
	endBlock := (offset + length) / cacheBlkSize

	// get block start offset
	var blockStartOffset int64
	if startBlock > 0 {
		blockStartOffset = (cacheBlkSize + int64(h.Size())) * startBlock
	}

	tillLength := (cacheBlkSize + int64(h.Size())) * (endBlock - startBlock + 1)

	// Start offset cannot be negative.
	if offset < 0 {
		logger.LogIf(ctx, errUnexpected)
		return errUnexpected
	}

	// Writer cannot be nil.
	if writer == nil {
		logger.LogIf(ctx, errUnexpected)
		return errUnexpected
	}
	var blockOffset, blockLength int64
	rc, err := readCacheFileStream(filePath, blockStartOffset, tillLength)
	if err != nil {
		return err
	}
	bufp := c.pool.Get().(*[]byte)
	defer c.pool.Put(bufp)

	for block := startBlock; block <= endBlock; block++ {
		switch {
		case startBlock == endBlock:
			blockOffset = offset % cacheBlkSize
			blockLength = length
		case block == startBlock:
			blockOffset = offset % cacheBlkSize
			blockLength = cacheBlkSize - blockOffset
		case block == endBlock:
			blockOffset = 0
			blockLength = (offset + length) % cacheBlkSize
		default:
			blockOffset = 0
			blockLength = cacheBlkSize
		}
		if blockLength == 0 {
			break
		}
		if _, err := io.ReadFull(rc, checksumHash); err != nil {
			return err
		}
		h.Reset()
		n, err := io.ReadFull(rc, *bufp)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			logger.LogIf(ctx, err)
			return err
		}
		eof := err == io.EOF || err == io.ErrUnexpectedEOF
		if n == 0 && length != 0 {
			// Reached EOF, nothing more to be done.
			break
		}

		if _, e := h.Write((*bufp)[:n]); e != nil {
			return e
		}
		hashBytes := h.Sum(nil)

		if !bytes.Equal(hashBytes, checksumHash) {
			err = fmt.Errorf("hashes do not match expected %s, got %s",
				hex.EncodeToString(checksumHash), hex.EncodeToString(hashBytes))
			logger.LogIf(GlobalContext, err)
			return err
		}

		if _, err := io.Copy(writer, bytes.NewReader((*bufp)[blockOffset:blockOffset+blockLength])); err != nil {
			if err != io.ErrClosedPipe {
				logger.LogIf(ctx, err)
				return err
			}
			eof = true
		}
		if eof {
			break
		}
	}

	return nil
}

// Get returns ObjectInfo and reader for object from disk cache
func (c *diskCache) Get(ctx context.Context, bucket, object string, rs *HTTPRangeSpec, h http.Header, opts ObjectOptions) (gr *GetObjectReader, numHits int, err error) {
	cacheObjPath := getCacheSHADir(c.dir, bucket, object)
	cLock := c.NewNSLockFn(cacheObjPath)
	lkctx, err := cLock.GetRLock(ctx, globalOperationTimeout)
	if err != nil {
		return nil, numHits, err
	}
	ctx = lkctx.Context()
	defer cLock.RUnlock(lkctx.Cancel)

	var objInfo ObjectInfo
	var rngInfo RangeInfo
	if objInfo, rngInfo, numHits, err = c.statRange(ctx, bucket, object, rs); err != nil {
		return nil, numHits, toObjectErr(err, bucket, object)
	}
	cacheFile := cacheDataFile
	objSize := objInfo.Size
	if !rngInfo.Empty() {
		// for cached ranges, need to pass actual range file size to GetObjectReader
		// and clear out range spec
		cacheFile = rngInfo.File
		objInfo.Size = rngInfo.Size
		rs = nil
	}

	// For a directory, we need to send an reader that returns no bytes.
	if HasSuffix(object, SlashSeparator) {
		// The lock taken above is released when
		// objReader.Close() is called by the caller.
		gr, gerr := NewGetObjectReaderFromReader(bytes.NewBuffer(nil), objInfo, opts)
		return gr, numHits, gerr
	}

	fn, off, length, nErr := NewGetObjectReader(rs, objInfo, opts)
	if nErr != nil {
		return nil, numHits, nErr
	}
	filePath := pathJoin(cacheObjPath, cacheFile)
	pr, pw := xioutil.WaitPipe()
	go func() {
		err := c.bitrotReadFromCache(ctx, filePath, off, length, pw)
		if err != nil {
			removeAll(cacheObjPath)
		}
		pw.CloseWithError(err)
	}()
	// Cleanup function to cause the go routine above to exit, in
	// case of incomplete read.
	pipeCloser := func() { pr.CloseWithError(nil) }

	gr, gerr := fn(pr, h, pipeCloser)
	if gerr != nil {
		return gr, numHits, gerr
	}
	if globalCacheKMS != nil {
		// clean up internal SSE cache metadata
		delete(gr.ObjInfo.UserDefined, xhttp.AmzServerSideEncryption)
	}
	if !rngInfo.Empty() {
		// overlay Size with actual object size and not the range size
		gr.ObjInfo.Size = objSize
	}
	return gr, numHits, nil

}

// Deletes the cached object
func (c *diskCache) delete(ctx context.Context, cacheObjPath string) (err error) {
	cLock := c.NewNSLockFn(cacheObjPath)
	lkctx, err := cLock.GetLock(ctx, globalOperationTimeout)
	if err != nil {
		return err
	}
	defer cLock.Unlock(lkctx.Cancel)
	return removeAll(cacheObjPath)
}

// Deletes the cached object
func (c *diskCache) Delete(ctx context.Context, bucket, object string) (err error) {
	cacheObjPath := getCacheSHADir(c.dir, bucket, object)
	return c.delete(ctx, cacheObjPath)
}

// convenience function to check if object is cached on this diskCache
func (c *diskCache) Exists(ctx context.Context, bucket, object string) bool {
	if _, err := os.Stat(getCacheSHADir(c.dir, bucket, object)); err != nil {
		return false
	}
	return true
}

// queues writeback upload failures on server startup
func (c *diskCache) scanCacheWritebackFailures(ctx context.Context) {
	defer close(c.retryWritebackCh)
	filterFn := func(name string, typ os.FileMode) error {
		if name == minioMetaBucket {
			// Proceed to next file.
			return nil
		}
		cacheDir := pathJoin(c.dir, name)
		meta, _, _, err := c.statCachedMeta(ctx, cacheDir)
		if err != nil {
			return nil
		}

		objInfo := meta.ToObjectInfo("", "")
		status, ok := objInfo.UserDefined[writeBackStatusHeader]
		if !ok || status == CommitComplete.String() {
			return nil
		}
		select {
		case c.retryWritebackCh <- objInfo:
		default:
		}

		return nil
	}

	if err := readDirFn(c.dir, filterFn); err != nil {
		logger.LogIf(ctx, err)
		return
	}
}
