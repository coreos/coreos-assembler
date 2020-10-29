/*
 * MinIO Cloud Storage, (C) 2018-2019 MinIO, Inc.
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
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math/rand"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/dchest/siphash"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7/pkg/tags"
	"github.com/minio/minio/cmd/config"
	"github.com/minio/minio/cmd/logger"
	"github.com/minio/minio/pkg/bpool"
	"github.com/minio/minio/pkg/dsync"
	"github.com/minio/minio/pkg/env"
	"github.com/minio/minio/pkg/madmin"
	"github.com/minio/minio/pkg/sync/errgroup"
)

// setsDsyncLockers is encapsulated type for Close()
type setsDsyncLockers [][]dsync.NetLocker

// Information of a new disk connection
type diskConnectInfo struct {
	setIndex int
}

// erasureSets implements ObjectLayer combining a static list of erasure coded
// object sets. NOTE: There is no dynamic scaling allowed or intended in
// current design.
type erasureSets struct {
	GatewayUnsupported

	sets []*erasureObjects

	// Reference format.
	format *formatErasureV3

	// erasureDisks mutex to lock erasureDisks.
	erasureDisksMu sync.RWMutex

	// Re-ordered list of disks per set.
	erasureDisks [][]StorageAPI

	// Distributed locker clients.
	erasureLockers setsDsyncLockers

	// Distributed lock owner (constant per running instance).
	erasureLockOwner string

	// List of endpoints provided on the command line.
	endpoints Endpoints

	// String version of all the endpoints, an optimization
	// to avoid url.String() conversion taking CPU on
	// large disk setups.
	endpointStrings []string

	// Total number of sets and the number of disks per set.
	setCount, setDriveCount int
	listTolerancePerSet     int

	monitorContextCancel context.CancelFunc
	disksConnectEvent    chan diskConnectInfo

	// Distribution algorithm of choice.
	distributionAlgo string
	deploymentID     [16]byte

	disksStorageInfoCache timedValue

	// Merge tree walk
	pool         *MergeWalkPool
	poolSplunk   *MergeWalkPool
	poolVersions *MergeWalkVersionsPool

	mrfMU         sync.Mutex
	mrfOperations map[healSource]int
}

func isEndpointConnected(diskMap map[string]StorageAPI, endpoint string) bool {
	disk := diskMap[endpoint]
	if disk == nil {
		return false
	}
	return disk.IsOnline()
}

func (s *erasureSets) getDiskMap() map[string]StorageAPI {
	diskMap := make(map[string]StorageAPI)

	s.erasureDisksMu.RLock()
	defer s.erasureDisksMu.RUnlock()

	for i := 0; i < s.setCount; i++ {
		for j := 0; j < s.setDriveCount; j++ {
			disk := s.erasureDisks[i][j]
			if disk == OfflineDisk {
				continue
			}
			if !disk.IsOnline() {
				continue
			}
			diskMap[disk.String()] = disk
		}
	}
	return diskMap
}

// Initializes a new StorageAPI from the endpoint argument, returns
// StorageAPI and also `format` which exists on the disk.
func connectEndpoint(endpoint Endpoint) (StorageAPI, *formatErasureV3, error) {
	disk, err := newStorageAPIWithoutHealthCheck(endpoint)
	if err != nil {
		return nil, nil, err
	}

	format, err := loadFormatErasure(disk)
	if err != nil {
		if errors.Is(err, errUnformattedDisk) {
			info, derr := disk.DiskInfo(context.TODO())
			if derr != nil && info.RootDisk {
				return nil, nil, fmt.Errorf("Disk: %s returned %w", disk, derr) // make sure to '%w' to wrap the error
			}
		}
		return nil, nil, fmt.Errorf("Disk: %s returned %w", disk, err) // make sure to '%w' to wrap the error
	}

	return disk, format, nil
}

// findDiskIndex - returns the i,j'th position of the input `diskID` against the reference
// format, after successful validation.
//   - i'th position is the set index
//   - j'th position is the disk index in the current set
func findDiskIndexByDiskID(refFormat *formatErasureV3, diskID string) (int, int, error) {
	if diskID == offlineDiskUUID {
		return -1, -1, fmt.Errorf("diskID: %s is offline", diskID)
	}
	for i := 0; i < len(refFormat.Erasure.Sets); i++ {
		for j := 0; j < len(refFormat.Erasure.Sets[0]); j++ {
			if refFormat.Erasure.Sets[i][j] == diskID {
				return i, j, nil
			}
		}
	}

	return -1, -1, fmt.Errorf("diskID: %s not found", diskID)
}

// findDiskIndex - returns the i,j'th position of the input `format` against the reference
// format, after successful validation.
//   - i'th position is the set index
//   - j'th position is the disk index in the current set
func findDiskIndex(refFormat, format *formatErasureV3) (int, int, error) {
	if err := formatErasureV3Check(refFormat, format); err != nil {
		return 0, 0, err
	}

	if format.Erasure.This == offlineDiskUUID {
		return -1, -1, fmt.Errorf("diskID: %s is offline", format.Erasure.This)
	}

	for i := 0; i < len(refFormat.Erasure.Sets); i++ {
		for j := 0; j < len(refFormat.Erasure.Sets[0]); j++ {
			if refFormat.Erasure.Sets[i][j] == format.Erasure.This {
				return i, j, nil
			}
		}
	}

	return -1, -1, fmt.Errorf("diskID: %s not found", format.Erasure.This)
}

// connectDisks - attempt to connect all the endpoints, loads format
// and re-arranges the disks in proper position.
func (s *erasureSets) connectDisks() {
	var wg sync.WaitGroup
	diskMap := s.getDiskMap()
	for _, endpoint := range s.endpoints {
		diskPath := endpoint.String()
		if endpoint.IsLocal {
			diskPath = endpoint.Path
		}
		if isEndpointConnected(diskMap, diskPath) {
			continue
		}
		wg.Add(1)
		go func(endpoint Endpoint) {
			defer wg.Done()
			disk, format, err := connectEndpoint(endpoint)
			if err != nil {
				if endpoint.IsLocal && errors.Is(err, errUnformattedDisk) {
					globalBackgroundHealState.pushHealLocalDisks(endpoint)
					logger.Info(fmt.Sprintf("Found unformatted drive %s, attempting to heal...", endpoint))
				} else {
					printEndpointError(endpoint, err, true)
				}
				return
			}
			if disk.IsLocal() && disk.Healing() {
				globalBackgroundHealState.pushHealLocalDisks(disk.Endpoint())
				logger.Info(fmt.Sprintf("Found the drive %s that needs healing, attempting to heal...", disk))
			}
			s.erasureDisksMu.RLock()
			setIndex, diskIndex, err := findDiskIndex(s.format, format)
			s.erasureDisksMu.RUnlock()
			if err != nil {
				printEndpointError(endpoint, err, false)
				return
			}

			s.erasureDisksMu.Lock()
			if s.erasureDisks[setIndex][diskIndex] != nil {
				s.erasureDisks[setIndex][diskIndex].Close()
			}
			if disk.IsLocal() {
				disk.SetDiskID(format.Erasure.This)
				s.erasureDisks[setIndex][diskIndex] = disk
			} else {
				// Enable healthcheck disk for remote endpoint.
				disk, err = newStorageAPI(endpoint)
				if err != nil {
					printEndpointError(endpoint, err, false)
					return
				}
				disk.SetDiskID(format.Erasure.This)
				s.erasureDisks[setIndex][diskIndex] = disk
			}
			s.endpointStrings[setIndex*s.setDriveCount+diskIndex] = disk.String()
			s.erasureDisksMu.Unlock()
			go func(setIndex int) {
				// Send a new disk connect event with a timeout
				select {
				case s.disksConnectEvent <- diskConnectInfo{setIndex: setIndex}:
				case <-time.After(100 * time.Millisecond):
				}
			}(setIndex)
		}(endpoint)
	}
	wg.Wait()
}

// monitorAndConnectEndpoints this is a monitoring loop to keep track of disconnected
// endpoints by reconnecting them and making sure to place them into right position in
// the set topology, this monitoring happens at a given monitoring interval.
func (s *erasureSets) monitorAndConnectEndpoints(ctx context.Context, monitorInterval time.Duration) {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	time.Sleep(time.Duration(r.Float64() * float64(time.Second)))

	// Pre-emptively connect the disks if possible.
	s.connectDisks()

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(monitorInterval):
			s.connectDisks()
		}
	}
}

// GetAllLockers return a list of all lockers for all sets.
func (s *erasureSets) GetAllLockers() []dsync.NetLocker {
	allLockers := make([]dsync.NetLocker, s.setDriveCount*s.setCount)
	for i, lockers := range s.erasureLockers {
		for j, locker := range lockers {
			allLockers[i*s.setDriveCount+j] = locker
		}
	}
	return allLockers
}

func (s *erasureSets) GetLockers(setIndex int) func() ([]dsync.NetLocker, string) {
	return func() ([]dsync.NetLocker, string) {
		lockers := make([]dsync.NetLocker, s.setDriveCount)
		copy(lockers, s.erasureLockers[setIndex])
		sort.Slice(lockers, func(i, j int) bool {
			// re-order lockers with affinity for
			// - non-local lockers
			// - online lockers
			// are used first
			return !lockers[i].IsLocal() && lockers[i].IsOnline()
		})
		return lockers, s.erasureLockOwner
	}
}

func (s *erasureSets) GetEndpoints(setIndex int) func() []string {
	return func() []string {
		s.erasureDisksMu.RLock()
		defer s.erasureDisksMu.RUnlock()

		eps := make([]string, s.setDriveCount)
		for i := 0; i < s.setDriveCount; i++ {
			eps[i] = s.endpointStrings[setIndex*s.setDriveCount+i]
		}
		return eps
	}
}

// GetDisks returns a closure for a given set, which provides list of disks per set.
func (s *erasureSets) GetDisks(setIndex int) func() []StorageAPI {
	return func() []StorageAPI {
		s.erasureDisksMu.RLock()
		defer s.erasureDisksMu.RUnlock()
		disks := make([]StorageAPI, s.setDriveCount)
		copy(disks, s.erasureDisks[setIndex])
		return disks
	}
}

// defaultMonitorConnectEndpointInterval is the interval to monitor endpoint connections.
// Must be bigger than defaultMonitorNewDiskInterval.
const defaultMonitorConnectEndpointInterval = defaultMonitorNewDiskInterval + time.Second*5

// Initialize new set of erasure coded sets.
func newErasureSets(ctx context.Context, endpoints Endpoints, storageDisks []StorageAPI, format *formatErasureV3) (*erasureSets, error) {
	setCount := len(format.Erasure.Sets)
	setDriveCount := len(format.Erasure.Sets[0])

	endpointStrings := make([]string, len(endpoints))

	listTolerancePerSet := 3
	// By default this is off
	if env.Get("MINIO_API_LIST_STRICT_QUORUM", config.EnableOn) == config.EnableOn {
		listTolerancePerSet = -1
	}

	// Initialize the erasure sets instance.
	s := &erasureSets{
		sets:                make([]*erasureObjects, setCount),
		erasureDisks:        make([][]StorageAPI, setCount),
		erasureLockers:      make([][]dsync.NetLocker, setCount),
		erasureLockOwner:    GetLocalPeer(globalEndpoints),
		endpoints:           endpoints,
		endpointStrings:     endpointStrings,
		setCount:            setCount,
		setDriveCount:       setDriveCount,
		listTolerancePerSet: listTolerancePerSet,
		format:              format,
		disksConnectEvent:   make(chan diskConnectInfo),
		distributionAlgo:    format.Erasure.DistributionAlgo,
		deploymentID:        uuid.MustParse(format.ID),
		pool:                NewMergeWalkPool(globalMergeLookupTimeout),
		poolSplunk:          NewMergeWalkPool(globalMergeLookupTimeout),
		poolVersions:        NewMergeWalkVersionsPool(globalMergeLookupTimeout),
		mrfOperations:       make(map[healSource]int),
	}

	mutex := newNSLock(globalIsDistErasure)

	// Initialize byte pool once for all sets, bpool size is set to
	// setCount * setDriveCount with each memory upto blockSizeV1.
	bp := bpool.NewBytePoolCap(setCount*setDriveCount, blockSizeV1, blockSizeV1*2)

	for i := 0; i < setCount; i++ {
		s.erasureDisks[i] = make([]StorageAPI, setDriveCount)
		s.erasureLockers[i] = make([]dsync.NetLocker, setDriveCount)
	}

	for i := 0; i < setCount; i++ {
		for j := 0; j < setDriveCount; j++ {
			endpoint := endpoints[i*setDriveCount+j]
			// Rely on endpoints list to initialize, init lockers and available disks.
			s.erasureLockers[i][j] = newLockAPI(endpoint)
			disk := storageDisks[i*setDriveCount+j]
			if disk == nil {
				continue
			}
			diskID, derr := disk.GetDiskID()
			if derr != nil {
				continue
			}
			m, n, err := findDiskIndexByDiskID(format, diskID)
			if err != nil {
				continue
			}
			s.endpointStrings[m*setDriveCount+n] = disk.String()
			s.erasureDisks[m][n] = disk
		}

		// Initialize erasure objects for a given set.
		s.sets[i] = &erasureObjects{
			getDisks:     s.GetDisks(i),
			getLockers:   s.GetLockers(i),
			getEndpoints: s.GetEndpoints(i),
			nsMutex:      mutex,
			bp:           bp,
			mrfOpCh:      make(chan partialOperation, 10000),
		}

		go s.sets[i].cleanupStaleUploads(ctx,
			GlobalStaleUploadsCleanupInterval, GlobalStaleUploadsExpiry)
	}

	mctx, mctxCancel := context.WithCancel(ctx)
	s.monitorContextCancel = mctxCancel

	// Start the disk monitoring and connect routine.
	go s.monitorAndConnectEndpoints(mctx, defaultMonitorConnectEndpointInterval)
	go s.maintainMRFList()
	go s.healMRFRoutine()

	return s, nil
}

// NewNSLock - initialize a new namespace RWLocker instance.
func (s *erasureSets) NewNSLock(ctx context.Context, bucket string, objects ...string) RWLocker {
	if len(objects) == 1 {
		return s.getHashedSet(objects[0]).NewNSLock(ctx, bucket, objects...)
	}
	return s.getHashedSet("").NewNSLock(ctx, bucket, objects...)
}

// SetDriveCount returns the current drives per set.
func (s *erasureSets) SetDriveCount() int {
	return s.setDriveCount
}

// StorageUsageInfo - combines output of StorageInfo across all erasure coded object sets.
// This only returns disk usage info for ServerSets to perform placement decision, this call
// is not implemented in Object interface and is not meant to be used by other object
// layer implementations.
func (s *erasureSets) StorageUsageInfo(ctx context.Context) StorageInfo {
	storageUsageInfo := func() StorageInfo {
		var storageInfo StorageInfo
		storageInfos := make([]StorageInfo, len(s.sets))
		storageInfo.Backend.Type = BackendErasure

		g := errgroup.WithNErrs(len(s.sets))
		for index := range s.sets {
			index := index
			g.Go(func() error {
				// ignoring errors on purpose
				storageInfos[index], _ = s.sets[index].StorageInfo(ctx, false)
				return nil
			}, index)
		}

		// Wait for the go routines.
		g.Wait()

		for _, lstorageInfo := range storageInfos {
			storageInfo.Disks = append(storageInfo.Disks, lstorageInfo.Disks...)
			storageInfo.Backend.OnlineDisks = storageInfo.Backend.OnlineDisks.Merge(lstorageInfo.Backend.OnlineDisks)
			storageInfo.Backend.OfflineDisks = storageInfo.Backend.OfflineDisks.Merge(lstorageInfo.Backend.OfflineDisks)
		}

		return storageInfo
	}

	s.disksStorageInfoCache.Once.Do(func() {
		s.disksStorageInfoCache.TTL = time.Second
		s.disksStorageInfoCache.Update = func() (interface{}, error) {
			return storageUsageInfo(), nil
		}
	})

	v, _ := s.disksStorageInfoCache.Get()
	return v.(StorageInfo)
}

// StorageInfo - combines output of StorageInfo across all erasure coded object sets.
func (s *erasureSets) StorageInfo(ctx context.Context, local bool) (StorageInfo, []error) {
	var storageInfo StorageInfo

	storageInfos := make([]StorageInfo, len(s.sets))
	storageInfoErrs := make([][]error, len(s.sets))

	g := errgroup.WithNErrs(len(s.sets))
	for index := range s.sets {
		index := index
		g.Go(func() error {
			storageInfos[index], storageInfoErrs[index] = s.sets[index].StorageInfo(ctx, local)
			return nil
		}, index)
	}

	// Wait for the go routines.
	g.Wait()

	for _, lstorageInfo := range storageInfos {
		storageInfo.Disks = append(storageInfo.Disks, lstorageInfo.Disks...)
		storageInfo.Backend.OnlineDisks = storageInfo.Backend.OnlineDisks.Merge(lstorageInfo.Backend.OnlineDisks)
		storageInfo.Backend.OfflineDisks = storageInfo.Backend.OfflineDisks.Merge(lstorageInfo.Backend.OfflineDisks)
	}

	if local {
		// if local is true, we are not interested in the drive UUID info.
		// this is called primarily by prometheus
		return storageInfo, nil
	}

	var errs []error
	for i := range s.sets {
		errs = append(errs, storageInfoErrs[i]...)
	}

	return storageInfo, errs
}

// Shutdown shutsdown all erasure coded sets in parallel
// returns error upon first error.
func (s *erasureSets) Shutdown(ctx context.Context) error {
	g := errgroup.WithNErrs(len(s.sets))

	for index := range s.sets {
		index := index
		g.Go(func() error {
			return s.sets[index].Shutdown(ctx)
		}, index)
	}

	for _, err := range g.Wait() {
		if err != nil {
			return err
		}
	}
	select {
	case _, ok := <-s.disksConnectEvent:
		if ok {
			close(s.disksConnectEvent)
		}
	default:
		close(s.disksConnectEvent)
	}
	return nil
}

// MakeBucketLocation - creates a new bucket across all sets simultaneously,
// then return the first encountered error
func (s *erasureSets) MakeBucketWithLocation(ctx context.Context, bucket string, opts BucketOptions) error {
	g := errgroup.WithNErrs(len(s.sets))

	// Create buckets in parallel across all sets.
	for index := range s.sets {
		index := index
		g.Go(func() error {
			return s.sets[index].MakeBucketWithLocation(ctx, bucket, opts)
		}, index)
	}

	errs := g.Wait()

	// Return the first encountered error
	for _, err := range errs {
		if err != nil {
			return err
		}
	}

	// Success.
	return nil
}

// hashes the key returning an integer based on the input algorithm.
// This function currently supports
// - CRCMOD
// - SIPMOD
// - all new algos.
func sipHashMod(key string, cardinality int, id [16]byte) int {
	if cardinality <= 0 {
		return -1
	}
	sip := siphash.New(id[:])
	sip.Write([]byte(key))
	return int(sip.Sum64() % uint64(cardinality))
}

func crcHashMod(key string, cardinality int) int {
	if cardinality <= 0 {
		return -1
	}
	keyCrc := crc32.Checksum([]byte(key), crc32.IEEETable)
	return int(keyCrc % uint32(cardinality))
}

func hashKey(algo string, key string, cardinality int, id [16]byte) int {
	switch algo {
	case formatErasureVersionV2DistributionAlgoLegacy:
		return crcHashMod(key, cardinality)
	case formatErasureVersionV3DistributionAlgo:
		return sipHashMod(key, cardinality, id)
	default:
		// Unknown algorithm returns -1, also if cardinality is lesser than 0.
		return -1
	}
}

// Returns always a same erasure coded set for a given input.
func (s *erasureSets) getHashedSetIndex(input string) int {
	return hashKey(s.distributionAlgo, input, len(s.sets), s.deploymentID)
}

// Returns always a same erasure coded set for a given input.
func (s *erasureSets) getHashedSet(input string) (set *erasureObjects) {
	return s.sets[s.getHashedSetIndex(input)]
}

// GetBucketInfo - returns bucket info from one of the erasure coded set.
func (s *erasureSets) GetBucketInfo(ctx context.Context, bucket string) (bucketInfo BucketInfo, err error) {
	return s.getHashedSet("").GetBucketInfo(ctx, bucket)
}

// IsNotificationSupported returns whether bucket notification is applicable for this layer.
func (s *erasureSets) IsNotificationSupported() bool {
	return s.getHashedSet("").IsNotificationSupported()
}

// IsListenSupported returns whether listen bucket notification is applicable for this layer.
func (s *erasureSets) IsListenSupported() bool {
	return true
}

// IsEncryptionSupported returns whether server side encryption is implemented for this layer.
func (s *erasureSets) IsEncryptionSupported() bool {
	return s.getHashedSet("").IsEncryptionSupported()
}

// IsCompressionSupported returns whether compression is applicable for this layer.
func (s *erasureSets) IsCompressionSupported() bool {
	return s.getHashedSet("").IsCompressionSupported()
}

func (s *erasureSets) IsTaggingSupported() bool {
	return true
}

// DeleteBucket - deletes a bucket on all sets simultaneously,
// even if one of the sets fail to delete buckets, we proceed to
// undo a successful operation.
func (s *erasureSets) DeleteBucket(ctx context.Context, bucket string, forceDelete bool) error {
	g := errgroup.WithNErrs(len(s.sets))

	// Delete buckets in parallel across all sets.
	for index := range s.sets {
		index := index
		g.Go(func() error {
			return s.sets[index].DeleteBucket(ctx, bucket, forceDelete)
		}, index)
	}

	errs := g.Wait()
	// For any failure, we attempt undo all the delete buckets operation
	// by creating buckets again on all sets which were successfully deleted.
	for _, err := range errs {
		if err != nil {
			undoDeleteBucketSets(ctx, bucket, s.sets, errs)
			return err
		}
	}

	// Delete all bucket metadata.
	deleteBucketMetadata(ctx, s, bucket)

	// Success.
	return nil
}

// This function is used to undo a successful DeleteBucket operation.
func undoDeleteBucketSets(ctx context.Context, bucket string, sets []*erasureObjects, errs []error) {
	g := errgroup.WithNErrs(len(sets))

	// Undo previous delete bucket on all underlying sets.
	for index := range sets {
		index := index
		g.Go(func() error {
			if errs[index] == nil {
				return sets[index].MakeBucketWithLocation(ctx, bucket, BucketOptions{})
			}
			return nil
		}, index)
	}

	g.Wait()
}

// List all buckets from one of the set, we are not doing merge
// sort here just for simplification. As per design it is assumed
// that all buckets are present on all sets.
func (s *erasureSets) ListBuckets(ctx context.Context) (buckets []BucketInfo, err error) {
	// Always lists from the same set signified by the empty string.
	return s.ListBucketsHeal(ctx)
}

// --- Object Operations ---

// GetObjectNInfo - returns object info and locked object ReadCloser
func (s *erasureSets) GetObjectNInfo(ctx context.Context, bucket, object string, rs *HTTPRangeSpec, h http.Header, lockType LockType, opts ObjectOptions) (gr *GetObjectReader, err error) {
	return s.getHashedSet(object).GetObjectNInfo(ctx, bucket, object, rs, h, lockType, opts)
}

// GetObject - reads an object from the hashedSet based on the object name.
func (s *erasureSets) GetObject(ctx context.Context, bucket, object string, startOffset int64, length int64, writer io.Writer, etag string, opts ObjectOptions) error {
	return s.getHashedSet(object).GetObject(ctx, bucket, object, startOffset, length, writer, etag, opts)
}

// PutObject - writes an object to hashedSet based on the object name.
func (s *erasureSets) PutObject(ctx context.Context, bucket string, object string, data *PutObjReader, opts ObjectOptions) (objInfo ObjectInfo, err error) {
	return s.getHashedSet(object).PutObject(ctx, bucket, object, data, opts)
}

// GetObjectInfo - reads object metadata from the hashedSet based on the object name.
func (s *erasureSets) GetObjectInfo(ctx context.Context, bucket, object string, opts ObjectOptions) (objInfo ObjectInfo, err error) {
	return s.getHashedSet(object).GetObjectInfo(ctx, bucket, object, opts)
}

// DeleteObject - deletes an object from the hashedSet based on the object name.
func (s *erasureSets) DeleteObject(ctx context.Context, bucket string, object string, opts ObjectOptions) (objInfo ObjectInfo, err error) {
	return s.getHashedSet(object).DeleteObject(ctx, bucket, object, opts)
}

// DeleteObjects - bulk delete of objects
// Bulk delete is only possible within one set. For that purpose
// objects are group by set first, and then bulk delete is invoked
// for each set, the error response of each delete will be returned
func (s *erasureSets) DeleteObjects(ctx context.Context, bucket string, objects []ObjectToDelete, opts ObjectOptions) ([]DeletedObject, []error) {
	type delObj struct {
		// Set index associated to this object
		setIndex int
		// Original index from the list of arguments
		// where this object is passed
		origIndex int
		// object to delete
		object ObjectToDelete
	}

	// Transform []delObj to the list of object names
	toNames := func(delObjs []delObj) []ObjectToDelete {
		objs := make([]ObjectToDelete, len(delObjs))
		for i, obj := range delObjs {
			objs[i] = obj.object
		}
		return objs
	}

	// The result of delete operation on all passed objects
	var delErrs = make([]error, len(objects))

	// The result of delete objects
	var delObjects = make([]DeletedObject, len(objects))

	// A map between a set and its associated objects
	var objSetMap = make(map[int][]delObj)

	// Group objects by set index
	for i, object := range objects {
		index := s.getHashedSetIndex(object.ObjectName)
		objSetMap[index] = append(objSetMap[index], delObj{setIndex: index, origIndex: i, object: object})
	}

	// Invoke bulk delete on objects per set and save
	// the result of the delete operation
	for _, objsGroup := range objSetMap {
		dobjects, errs := s.getHashedSet(objsGroup[0].object.ObjectName).DeleteObjects(ctx, bucket, toNames(objsGroup), opts)
		for i, obj := range objsGroup {
			delErrs[obj.origIndex] = errs[i]
			if delErrs[obj.origIndex] == nil {
				delObjects[obj.origIndex] = dobjects[i]
			}
		}
	}

	return delObjects, delErrs
}

// CopyObject - copies objects from one hashedSet to another hashedSet, on server side.
func (s *erasureSets) CopyObject(ctx context.Context, srcBucket, srcObject, dstBucket, dstObject string, srcInfo ObjectInfo, srcOpts, dstOpts ObjectOptions) (objInfo ObjectInfo, err error) {
	srcSet := s.getHashedSet(srcObject)
	dstSet := s.getHashedSet(dstObject)

	cpSrcDstSame := srcSet == dstSet

	// Check if this request is only metadata update.
	if cpSrcDstSame && srcInfo.metadataOnly {

		// Version ID is set for the destination and source == destination version ID.
		// perform an in-place update.
		if dstOpts.VersionID != "" && srcOpts.VersionID == dstOpts.VersionID {
			return srcSet.CopyObject(ctx, srcBucket, srcObject, dstBucket, dstObject, srcInfo, srcOpts, dstOpts)
		}
		// Destination is not versioned and source version ID is empty
		// perform an in-place update.
		if !dstOpts.Versioned && srcOpts.VersionID == "" {
			return srcSet.CopyObject(ctx, srcBucket, srcObject, dstBucket, dstObject, srcInfo, srcOpts, dstOpts)
		}
		// CopyObject optimization where we don't create an entire copy
		// of the content, instead we add a reference, we disallow legacy
		// objects to be self referenced in this manner so make sure
		// that we actually create a new dataDir for legacy objects.
		if dstOpts.Versioned && srcOpts.VersionID != dstOpts.VersionID && !srcInfo.Legacy {
			srcInfo.versionOnly = true
			return srcSet.CopyObject(ctx, srcBucket, srcObject, dstBucket, dstObject, srcInfo, srcOpts, dstOpts)
		}
	}

	putOpts := ObjectOptions{
		ServerSideEncryption: dstOpts.ServerSideEncryption,
		UserDefined:          srcInfo.UserDefined,
		Versioned:            dstOpts.Versioned,
		VersionID:            dstOpts.VersionID,
	}

	return dstSet.putObject(ctx, dstBucket, dstObject, srcInfo.PutObjReader, putOpts)
}

// FileInfoVersionsCh - file info versions channel
type FileInfoVersionsCh struct {
	Ch    chan FileInfoVersions
	Prev  FileInfoVersions
	Valid bool
}

// Pop - pops a cached entry if any, or from the cached channel.
func (f *FileInfoVersionsCh) Pop() (fi FileInfoVersions, ok bool) {
	if f.Valid {
		f.Valid = false
		return f.Prev, true
	} // No cached entries found, read from channel
	f.Prev, ok = <-f.Ch
	return f.Prev, ok
}

// Push - cache an entry, for Pop() later.
func (f *FileInfoVersionsCh) Push(fi FileInfoVersions) {
	f.Prev = fi
	f.Valid = true
}

// FileInfoCh - file info channel
type FileInfoCh struct {
	Ch    chan FileInfo
	Prev  FileInfo
	Valid bool
}

// Pop - pops a cached entry if any, or from the cached channel.
func (f *FileInfoCh) Pop() (fi FileInfo, ok bool) {
	if f.Valid {
		f.Valid = false
		return f.Prev, true
	} // No cached entries found, read from channel
	f.Prev, ok = <-f.Ch
	return f.Prev, ok
}

// Push - cache an entry, for Pop() later.
func (f *FileInfoCh) Push(fi FileInfo) {
	f.Prev = fi
	f.Valid = true
}

// Calculate lexically least entry across multiple FileInfo channels,
// returns the lexically common entry and the total number of times
// we found this entry. Additionally also returns a boolean
// to indicate if the caller needs to call this function
// again to list the next entry. It is callers responsibility
// if the caller wishes to list N entries to call lexicallySortedEntry
// N times until this boolean is 'false'.
func lexicallySortedEntryVersions(entryChs []FileInfoVersionsCh, entries []FileInfoVersions, entriesValid []bool) (FileInfoVersions, int, bool) {
	for j := range entryChs {
		entries[j], entriesValid[j] = entryChs[j].Pop()
	}

	var isTruncated = false
	for _, valid := range entriesValid {
		if !valid {
			continue
		}
		isTruncated = true
		break
	}

	var lentry FileInfoVersions
	var found bool
	for i, valid := range entriesValid {
		if !valid {
			continue
		}
		if !found {
			lentry = entries[i]
			found = true
			continue
		}
		if entries[i].Name < lentry.Name {
			lentry = entries[i]
		}
	}

	// We haven't been able to find any lexically least entry,
	// this would mean that we don't have valid entry.
	if !found {
		return lentry, 0, isTruncated
	}

	lexicallySortedEntryCount := 0
	for i, valid := range entriesValid {
		if !valid {
			continue
		}

		// Entries are duplicated across disks,
		// we should simply skip such entries.
		if lentry.Name == entries[i].Name && lentry.LatestModTime.Equal(entries[i].LatestModTime) {
			lexicallySortedEntryCount++
			continue
		}

		// Push all entries which are lexically higher
		// and will be returned later in Pop()
		entryChs[i].Push(entries[i])
	}

	return lentry, lexicallySortedEntryCount, isTruncated
}

func (s *erasureSets) startMergeWalks(ctx context.Context, bucket, prefix, marker string, recursive bool, endWalkCh <-chan struct{}) []FileInfoCh {
	return s.startMergeWalksN(ctx, bucket, prefix, marker, recursive, endWalkCh, -1, false)
}

func (s *erasureSets) startMergeWalksVersions(ctx context.Context, bucket, prefix, marker string, recursive bool, endWalkCh <-chan struct{}) []FileInfoVersionsCh {
	return s.startMergeWalksVersionsN(ctx, bucket, prefix, marker, recursive, endWalkCh, -1)
}

// Starts a walk versions channel across N number of disks and returns a slice.
// FileInfoCh which can be read from.
func (s *erasureSets) startMergeWalksVersionsN(ctx context.Context, bucket, prefix, marker string, recursive bool, endWalkCh <-chan struct{}, ndisks int) []FileInfoVersionsCh {
	var entryChs []FileInfoVersionsCh
	var wg sync.WaitGroup
	var mutex sync.Mutex
	for _, set := range s.sets {
		// Reset for the next erasure set.
		for _, disk := range set.getLoadBalancedNDisks(ndisks) {
			wg.Add(1)
			go func(disk StorageAPI) {
				defer wg.Done()

				entryCh, err := disk.WalkVersions(GlobalContext, bucket, prefix, marker, recursive, endWalkCh)
				if err != nil {
					return
				}

				mutex.Lock()
				entryChs = append(entryChs, FileInfoVersionsCh{
					Ch: entryCh,
				})
				mutex.Unlock()
			}(disk)
		}
	}
	wg.Wait()
	return entryChs
}

// Starts a walk channel across n number of disks and returns a slice of
// FileInfoCh which can be read from.
func (s *erasureSets) startMergeWalksN(ctx context.Context, bucket, prefix, marker string, recursive bool, endWalkCh <-chan struct{}, ndisks int, splunk bool) []FileInfoCh {
	var entryChs []FileInfoCh
	var wg sync.WaitGroup
	var mutex sync.Mutex
	for _, set := range s.sets {
		// Reset for the next erasure set.
		for _, disk := range set.getLoadBalancedNDisks(ndisks) {
			wg.Add(1)
			go func(disk StorageAPI) {
				defer wg.Done()

				var entryCh chan FileInfo
				var err error
				if splunk {
					entryCh, err = disk.WalkSplunk(GlobalContext, bucket, prefix, marker, endWalkCh)
				} else {
					entryCh, err = disk.Walk(GlobalContext, bucket, prefix, marker, recursive, endWalkCh)
				}
				if err != nil {
					// Disk walk returned error, ignore it.
					return
				}
				mutex.Lock()
				entryChs = append(entryChs, FileInfoCh{
					Ch: entryCh,
				})
				mutex.Unlock()
			}(disk)
		}
	}
	wg.Wait()
	return entryChs
}

func (s *erasureSets) ListMultipartUploads(ctx context.Context, bucket, prefix, keyMarker, uploadIDMarker, delimiter string, maxUploads int) (result ListMultipartsInfo, err error) {
	// In list multipart uploads we are going to treat input prefix as the object,
	// this means that we are not supporting directory navigation.
	return s.getHashedSet(prefix).ListMultipartUploads(ctx, bucket, prefix, keyMarker, uploadIDMarker, delimiter, maxUploads)
}

// Initiate a new multipart upload on a hashedSet based on object name.
func (s *erasureSets) NewMultipartUpload(ctx context.Context, bucket, object string, opts ObjectOptions) (uploadID string, err error) {
	return s.getHashedSet(object).NewMultipartUpload(ctx, bucket, object, opts)
}

// Copies a part of an object from source hashedSet to destination hashedSet.
func (s *erasureSets) CopyObjectPart(ctx context.Context, srcBucket, srcObject, destBucket, destObject string, uploadID string, partID int,
	startOffset int64, length int64, srcInfo ObjectInfo, srcOpts, dstOpts ObjectOptions) (partInfo PartInfo, err error) {
	destSet := s.getHashedSet(destObject)

	return destSet.PutObjectPart(ctx, destBucket, destObject, uploadID, partID, NewPutObjReader(srcInfo.Reader, nil, nil), dstOpts)
}

// PutObjectPart - writes part of an object to hashedSet based on the object name.
func (s *erasureSets) PutObjectPart(ctx context.Context, bucket, object, uploadID string, partID int, data *PutObjReader, opts ObjectOptions) (info PartInfo, err error) {
	return s.getHashedSet(object).PutObjectPart(ctx, bucket, object, uploadID, partID, data, opts)
}

// GetMultipartInfo - return multipart metadata info uploaded at hashedSet.
func (s *erasureSets) GetMultipartInfo(ctx context.Context, bucket, object, uploadID string, opts ObjectOptions) (result MultipartInfo, err error) {
	return s.getHashedSet(object).GetMultipartInfo(ctx, bucket, object, uploadID, opts)
}

// ListObjectParts - lists all uploaded parts to an object in hashedSet.
func (s *erasureSets) ListObjectParts(ctx context.Context, bucket, object, uploadID string, partNumberMarker int, maxParts int, opts ObjectOptions) (result ListPartsInfo, err error) {
	return s.getHashedSet(object).ListObjectParts(ctx, bucket, object, uploadID, partNumberMarker, maxParts, opts)
}

// Aborts an in-progress multipart operation on hashedSet based on the object name.
func (s *erasureSets) AbortMultipartUpload(ctx context.Context, bucket, object, uploadID string, opts ObjectOptions) error {
	return s.getHashedSet(object).AbortMultipartUpload(ctx, bucket, object, uploadID, opts)
}

// CompleteMultipartUpload - completes a pending multipart transaction, on hashedSet based on object name.
func (s *erasureSets) CompleteMultipartUpload(ctx context.Context, bucket, object, uploadID string, uploadedParts []CompletePart, opts ObjectOptions) (objInfo ObjectInfo, err error) {
	return s.getHashedSet(object).CompleteMultipartUpload(ctx, bucket, object, uploadID, uploadedParts, opts)
}

/*

All disks online
-----------------
- All Unformatted - format all and return success.
- Some Unformatted - format all and return success.
- Any JBOD inconsistent - return failure
- Some are corrupt (missing format.json) - return failure
- Any unrecognized disks - return failure

Some disks are offline and we have quorum.
-----------------
- Some unformatted - format all and return success,
  treat disks offline as corrupted.
- Any JBOD inconsistent - return failure
- Some are corrupt (missing format.json)
- Any unrecognized disks - return failure

No read quorum
-----------------
failure for all cases.

// Pseudo code for managing `format.json`.

// Generic checks.
if (no quorum) return error
if (any disk is corrupt) return error // Always error
if (jbod inconsistent) return error // Always error.
if (disks not recognized) // Always error.

// Specific checks.
if (all disks online)
  if (all disks return format.json)
     if (jbod consistent)
        if (all disks recognized)
          return
  else
     if (all disks return format.json not found)
        return error
     else (some disks return format.json not found)
        (heal format)
        return
     fi
   fi
else
   if (some disks return format.json not found)
        // Offline disks are marked as dead.
        (heal format) // Offline disks should be marked as dead.
        return success
   fi
fi
*/

func formatsToDrivesInfo(endpoints Endpoints, formats []*formatErasureV3, sErrs []error) (beforeDrives []madmin.HealDriveInfo) {
	beforeDrives = make([]madmin.HealDriveInfo, len(endpoints))
	// Existing formats are available (i.e. ok), so save it in
	// result, also populate disks to be healed.
	for i, format := range formats {
		drive := endpoints.GetString(i)
		var state = madmin.DriveStateCorrupt
		switch {
		case format != nil:
			state = madmin.DriveStateOk
		case sErrs[i] == errUnformattedDisk:
			state = madmin.DriveStateMissing
		case sErrs[i] == errDiskNotFound:
			state = madmin.DriveStateOffline
		}
		beforeDrives[i] = madmin.HealDriveInfo{
			UUID: func() string {
				if format != nil {
					return format.Erasure.This
				}
				return ""
			}(),
			Endpoint: drive,
			State:    state,
		}
	}

	return beforeDrives
}

// Reloads the format from the disk, usually called by a remote peer notifier while
// healing in a distributed setup.
func (s *erasureSets) ReloadFormat(ctx context.Context, dryRun bool) (err error) {
	storageDisks, errs := initStorageDisksWithErrorsWithoutHealthCheck(s.endpoints)
	for i, err := range errs {
		if err != nil && err != errDiskNotFound {
			return fmt.Errorf("Disk %s: %w", s.endpoints[i], err)
		}
	}
	defer func(storageDisks []StorageAPI) {
		if err != nil {
			closeStorageDisks(storageDisks)
		}
	}(storageDisks)

	formats, _ := loadFormatErasureAll(storageDisks, false)
	if err = checkFormatErasureValues(formats, s.setDriveCount); err != nil {
		return err
	}

	refFormat, err := getFormatErasureInQuorum(formats)
	if err != nil {
		return err
	}

	s.monitorContextCancel() // turn-off disk monitoring and replace format.

	s.erasureDisksMu.Lock()

	// Replace with new reference format.
	s.format = refFormat

	// Close all existing disks and reconnect all the disks.
	for _, disk := range storageDisks {
		if disk == nil {
			continue
		}

		diskID, err := disk.GetDiskID()
		if err != nil {
			continue
		}

		m, n, err := findDiskIndexByDiskID(refFormat, diskID)
		if err != nil {
			continue
		}

		if s.erasureDisks[m][n] != nil {
			s.erasureDisks[m][n].Close()
		}

		s.endpointStrings[m*s.setDriveCount+n] = disk.String()
		if !disk.IsLocal() {
			// Enable healthcheck disk for remote endpoint.
			disk, err = newStorageAPI(disk.Endpoint())
			if err != nil {
				continue
			}
			disk.SetDiskID(diskID)
		}

		s.erasureDisks[m][n] = disk
	}

	s.erasureDisksMu.Unlock()

	mctx, mctxCancel := context.WithCancel(GlobalContext)
	s.monitorContextCancel = mctxCancel

	go s.monitorAndConnectEndpoints(mctx, defaultMonitorConnectEndpointInterval)

	return nil
}

// If it is a single node Erasure and all disks are root disks, it is most likely a test setup, else it is a production setup.
// On a test setup we allow creation of format.json on root disks to help with dev/testing.
func isTestSetup(infos []DiskInfo, errs []error) bool {
	rootDiskCount := 0
	for i := range errs {
		if errs[i] == nil || errs[i] == errUnformattedDisk {
			if infos[i].RootDisk {
				rootDiskCount++
			}
		}
	}
	// It is a test setup if all disks are root disks in quorum.
	return rootDiskCount >= len(infos)/2+1
}

func getHealDiskInfos(storageDisks []StorageAPI, errs []error) ([]DiskInfo, []error) {
	infos := make([]DiskInfo, len(storageDisks))
	g := errgroup.WithNErrs(len(storageDisks))
	for index := range storageDisks {
		index := index
		g.Go(func() error {
			if errs[index] != nil && errs[index] != errUnformattedDisk {
				return errs[index]
			}
			if storageDisks[index] == nil {
				return errDiskNotFound
			}
			var err error
			infos[index], err = storageDisks[index].DiskInfo(context.TODO())
			return err
		}, index)
	}
	return infos, g.Wait()
}

// Mark root disks as down so as not to heal them.
func markRootDisksAsDown(storageDisks []StorageAPI, errs []error) {
	var infos []DiskInfo
	infos, errs = getHealDiskInfos(storageDisks, errs)
	if !isTestSetup(infos, errs) {
		for i := range storageDisks {
			if storageDisks[i] != nil && infos[i].RootDisk {
				// We should not heal on root disk. i.e in a situation where the minio-administrator has unmounted a
				// defective drive we should not heal a path on the root disk.
				logger.Info("Disk `%s` is a root disk. Please ensure the disk is mounted properly, refusing to use root disk.",
					storageDisks[i].String())
				storageDisks[i] = nil
			}
		}
	}
}

// HealFormat - heals missing `format.json` on fresh unformatted disks.
func (s *erasureSets) HealFormat(ctx context.Context, dryRun bool) (res madmin.HealResultItem, err error) {
	storageDisks, errs := initStorageDisksWithErrorsWithoutHealthCheck(s.endpoints)
	for i, derr := range errs {
		if derr != nil && derr != errDiskNotFound {
			return madmin.HealResultItem{}, fmt.Errorf("Disk %s: %w", s.endpoints[i], derr)
		}
	}

	defer func(storageDisks []StorageAPI) {
		if err != nil {
			closeStorageDisks(storageDisks)
		}
	}(storageDisks)

	formats, sErrs := loadFormatErasureAll(storageDisks, true)
	if err = checkFormatErasureValues(formats, s.setDriveCount); err != nil {
		return madmin.HealResultItem{}, err
	}

	// Mark all root disks down
	markRootDisksAsDown(storageDisks, sErrs)

	refFormat, err := getFormatErasureInQuorum(formats)
	if err != nil {
		return res, err
	}

	// Prepare heal-result
	res = madmin.HealResultItem{
		Type:      madmin.HealItemMetadata,
		Detail:    "disk-format",
		DiskCount: s.setCount * s.setDriveCount,
		SetCount:  s.setCount,
	}

	// Fetch all the drive info status.
	beforeDrives := formatsToDrivesInfo(s.endpoints, formats, sErrs)

	res.After.Drives = make([]madmin.HealDriveInfo, len(beforeDrives))
	res.Before.Drives = make([]madmin.HealDriveInfo, len(beforeDrives))
	// Copy "after" drive state too from before.
	for k, v := range beforeDrives {
		res.Before.Drives[k] = madmin.HealDriveInfo(v)
		res.After.Drives[k] = madmin.HealDriveInfo(v)
	}

	if countErrs(sErrs, errUnformattedDisk) == 0 {
		return res, errNoHealRequired
	}

	// Initialize a new set of set formats which will be written to disk.
	newFormatSets := newHealFormatSets(refFormat, s.setCount, s.setDriveCount, formats, sErrs)

	if !dryRun {
		var tmpNewFormats = make([]*formatErasureV3, s.setCount*s.setDriveCount)
		for i := range newFormatSets {
			for j := range newFormatSets[i] {
				if newFormatSets[i][j] == nil {
					continue
				}
				res.After.Drives[i*s.setDriveCount+j].UUID = newFormatSets[i][j].Erasure.This
				res.After.Drives[i*s.setDriveCount+j].State = madmin.DriveStateOk
				tmpNewFormats[i*s.setDriveCount+j] = newFormatSets[i][j]
			}
		}

		// Save formats `format.json` across all disks.
		if err = saveFormatErasureAllWithErrs(ctx, storageDisks, sErrs, tmpNewFormats); err != nil {
			return madmin.HealResultItem{}, err
		}

		refFormat, err = getFormatErasureInQuorum(tmpNewFormats)
		if err != nil {
			return madmin.HealResultItem{}, err
		}

		s.monitorContextCancel() // turn-off disk monitoring and replace format.

		s.erasureDisksMu.Lock()

		// Replace with new reference format.
		s.format = refFormat

		// Disconnect/relinquish all existing disks, lockers and reconnect the disks, lockers.
		for _, disk := range storageDisks {
			if disk == nil {
				continue
			}

			diskID, err := disk.GetDiskID()
			if err != nil {
				continue
			}

			m, n, err := findDiskIndexByDiskID(refFormat, diskID)
			if err != nil {
				continue
			}

			if s.erasureDisks[m][n] != nil {
				s.erasureDisks[m][n].Close()
			}

			s.endpointStrings[m*s.setDriveCount+n] = disk.String()
			if !disk.IsLocal() {
				// Enable healthcheck disk for remote endpoint.
				disk, err = newStorageAPI(disk.Endpoint())
				if err != nil {
					continue
				}
				disk.SetDiskID(diskID)
			}
			s.erasureDisks[m][n] = disk

		}

		s.erasureDisksMu.Unlock()

		mctx, mctxCancel := context.WithCancel(GlobalContext)
		s.monitorContextCancel = mctxCancel
		go s.monitorAndConnectEndpoints(mctx, defaultMonitorConnectEndpointInterval)
	}

	return res, nil
}

// HealBucket - heals inconsistent buckets and bucket metadata on all sets.
func (s *erasureSets) HealBucket(ctx context.Context, bucket string, dryRun, remove bool) (result madmin.HealResultItem, err error) {
	// Initialize heal result info
	result = madmin.HealResultItem{
		Type:      madmin.HealItemBucket,
		Bucket:    bucket,
		DiskCount: s.setCount * s.setDriveCount,
		SetCount:  s.setCount,
	}

	for _, s := range s.sets {
		var healResult madmin.HealResultItem
		healResult, err = s.HealBucket(ctx, bucket, dryRun, remove)
		if err != nil {
			return result, err
		}
		result.Before.Drives = append(result.Before.Drives, healResult.Before.Drives...)
		result.After.Drives = append(result.After.Drives, healResult.After.Drives...)
	}

	// Check if we had quorum to write, if not return an appropriate error.
	_, afterDriveOnline := result.GetOnlineCounts()
	if afterDriveOnline < ((s.setCount*s.setDriveCount)/2)+1 {
		return result, toObjectErr(errErasureWriteQuorum, bucket)
	}

	return result, nil
}

// HealObject - heals inconsistent object on a hashedSet based on object name.
func (s *erasureSets) HealObject(ctx context.Context, bucket, object, versionID string, opts madmin.HealOpts) (madmin.HealResultItem, error) {
	return s.getHashedSet(object).HealObject(ctx, bucket, object, versionID, opts)
}

// Lists all buckets which need healing.
func (s *erasureSets) ListBucketsHeal(ctx context.Context) ([]BucketInfo, error) {
	var listBuckets []BucketInfo
	var healBuckets = map[string]VolInfo{}
	for _, set := range s.sets {
		// lists all unique buckets across drives.
		if err := listAllBuckets(ctx, set.getDisks(), healBuckets); err != nil {
			return nil, err
		}
	}
	for _, v := range healBuckets {
		listBuckets = append(listBuckets, BucketInfo(v))
	}
	sort.Sort(byBucketName(listBuckets))
	return listBuckets, nil
}

// PutObjectTags - replace or add tags to an existing object
func (s *erasureSets) PutObjectTags(ctx context.Context, bucket, object string, tags string, opts ObjectOptions) error {
	return s.getHashedSet(object).PutObjectTags(ctx, bucket, object, tags, opts)
}

// DeleteObjectTags - delete object tags from an existing object
func (s *erasureSets) DeleteObjectTags(ctx context.Context, bucket, object string, opts ObjectOptions) error {
	return s.getHashedSet(object).DeleteObjectTags(ctx, bucket, object, opts)
}

// GetObjectTags - get object tags from an existing object
func (s *erasureSets) GetObjectTags(ctx context.Context, bucket, object string, opts ObjectOptions) (*tags.Tags, error) {
	return s.getHashedSet(object).GetObjectTags(ctx, bucket, object, opts)
}

// maintainMRFList gathers the list of successful partial uploads
// from all underlying er.sets and puts them in a global map which
// should not have more than 10000 entries.
func (s *erasureSets) maintainMRFList() {
	var agg = make(chan partialOperation, 10000)
	for i, er := range s.sets {
		go func(c <-chan partialOperation, setIndex int) {
			for msg := range c {
				msg.failedSet = setIndex
				select {
				case agg <- msg:
				default:
				}
			}
		}(er.mrfOpCh, i)
	}

	for fOp := range agg {
		s.mrfMU.Lock()
		if len(s.mrfOperations) > 10000 {
			s.mrfMU.Unlock()
			continue
		}
		s.mrfOperations[healSource{
			bucket:    fOp.bucket,
			object:    fOp.object,
			versionID: fOp.versionID,
		}] = fOp.failedSet
		s.mrfMU.Unlock()
	}
}

// healMRFRoutine monitors new disks connection, sweep the MRF list
// to find objects related to the new disk that needs to be healed.
func (s *erasureSets) healMRFRoutine() {
	// Wait until background heal state is initialized
	var bgSeq *healSequence
	for {
		if globalBackgroundHealState == nil {
			time.Sleep(time.Second)
			continue
		}
		var ok bool
		bgSeq, ok = globalBackgroundHealState.getHealSequenceByToken(bgHealingUUID)
		if ok {
			break
		}
		time.Sleep(time.Second)
	}

	for e := range s.disksConnectEvent {
		// Get the list of objects related the er.set
		// to which the connected disk belongs.
		var mrfOperations []healSource
		s.mrfMU.Lock()
		for k, v := range s.mrfOperations {
			if v == e.setIndex {
				mrfOperations = append(mrfOperations, k)
			}
		}
		s.mrfMU.Unlock()

		// Heal objects
		for _, u := range mrfOperations {
			// Send an object to be healed with a timeout
			select {
			case bgSeq.sourceCh <- u:
			case <-time.After(100 * time.Millisecond):
			}

			s.mrfMU.Lock()
			delete(s.mrfOperations, u)
			s.mrfMU.Unlock()
		}
	}
}
