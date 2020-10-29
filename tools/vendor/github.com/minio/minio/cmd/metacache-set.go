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
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/minio/minio/cmd/config/storageclass"
	xhttp "github.com/minio/minio/cmd/http"
	"github.com/minio/minio/cmd/logger"
	"github.com/minio/minio/pkg/console"
	"github.com/minio/minio/pkg/hash"
)

type listPathOptions struct {
	// ID of the listing.
	// This will be used to persist the list.
	ID string

	// Bucket of the listing.
	Bucket string

	// Directory inside the bucket.
	BaseDir string

	// Scan/return only content with prefix.
	Prefix string

	// Marker to resume listing.
	// The response will be the first entry AFTER this object name.
	Marker string

	// Limit the number of results.
	Limit int

	// The number of disks to ask. Special values:
	// 0 uses default number of disks.
	// -1 use at least 50% of disks or at least the default number.
	AskDisks int

	// InclDeleted will keep all entries where latest version is a delete marker.
	InclDeleted bool

	// Scan recursively.
	// If false only main directory will be scanned.
	// Should always be true if Separator is n SlashSeparator.
	Recursive bool

	// Separator to use.
	Separator string

	// Create indicates that the lister should not attempt to load an existing cache.
	Create bool

	// CurrentCycle indicates the current bloom cycle.
	// Will be used if a new scan is started.
	CurrentCycle uint64

	// OldestCycle indicates the oldest cycle acceptable.
	OldestCycle uint64

	// Include pure directories.
	IncludeDirectories bool

	// Transient is set if the cache is transient due to an error or being a reserved bucket.
	// This means the cache metadata will not be persisted on disk.
	// A transient result will never be returned from the cache so knowing the list id is required.
	Transient bool
}

func init() {
	gob.Register(listPathOptions{})
}

// gatherResults will collect all results on the input channel and filter results according to the options.
// Caller should close the channel when done.
// The returned function will return the results once there is enough or input is closed.
func (o *listPathOptions) gatherResults(in <-chan metaCacheEntry) func() (metaCacheEntriesSorted, error) {
	const debugPrint = false
	var resultsDone = make(chan metaCacheEntriesSorted)
	// Copy so we can mutate
	resCh := resultsDone
	resErr := io.EOF

	go func() {
		var results metaCacheEntriesSorted
		for entry := range in {
			if resCh == nil {
				// past limit
				continue
			}
			if !o.IncludeDirectories && entry.isDir() {
				continue
			}
			if debugPrint {
				console.Infoln("gather got:", entry.name)
			}
			if o.Marker != "" && entry.name <= o.Marker {
				if debugPrint {
					console.Infoln("pre marker")
				}
				continue
			}
			if !strings.HasPrefix(entry.name, o.Prefix) {
				if debugPrint {
					console.Infoln("not in prefix")
				}
				continue
			}
			if !o.Recursive && !entry.isInDir(o.Prefix, o.Separator) {
				if debugPrint {
					console.Infoln("not in dir", o.Prefix, o.Separator)
				}
				continue
			}
			if !o.InclDeleted && entry.isObject() {
				if entry.isLatestDeletemarker() {
					if debugPrint {
						console.Infoln("latest delete")
					}
					continue
				}
			}
			if o.Limit > 0 && results.len() >= o.Limit {
				// We have enough and we have more.
				// Do not return io.EOF
				if resCh != nil {
					resErr = nil
					resCh <- results
					resCh = nil
				}
				continue
			}
			if debugPrint {
				console.Infoln("adding...")
			}
			results.o = append(results.o, entry)
		}
		if resCh != nil {
			resErr = io.EOF
			resCh <- results
		}
	}()
	return func() (metaCacheEntriesSorted, error) {
		return <-resultsDone, resErr
	}
}

// findFirstPart will find the part with 0 being the first that corresponds to the marker in the options.
// io.ErrUnexpectedEOF is returned if the place containing the marker hasn't been scanned yet.
// io.EOF indicates the marker is beyond the end of the stream and does not exist.
func (o *listPathOptions) findFirstPart(fi FileInfo) (int, error) {
	search := o.Marker
	if search == "" {
		search = o.Prefix
	}
	if search == "" {
		return 0, nil
	}
	const debugPrint = false
	if debugPrint {
		console.Infoln("searching for ", search)
	}
	var tmp metacacheBlock
	i := 0
	for {
		partKey := fmt.Sprintf("%s-metacache-part-%d", ReservedMetadataPrefixLower, i)
		v, ok := fi.Metadata[partKey]
		if !ok {
			if debugPrint {
				console.Infoln("no match in metadata, waiting")
			}
			return -1, io.ErrUnexpectedEOF
		}
		err := json.Unmarshal([]byte(v), &tmp)
		if !ok {
			logger.LogIf(context.Background(), err)
			return -1, err
		}
		if tmp.First == "" && tmp.Last == "" && tmp.EOS {
			return 0, errFileNotFound
		}
		if tmp.First >= search {
			if debugPrint {
				console.Infoln("First >= search", v)
			}
			return i, nil
		}
		if tmp.Last >= search {
			if debugPrint {

				console.Infoln("Last >= search", v)
			}
			return i, nil
		}
		if tmp.EOS {
			if debugPrint {
				console.Infoln("no match, at EOS", v)
			}
			return -3, io.EOF
		}
		if debugPrint {
			console.Infoln("First ", tmp.First, "<", search, " search", i)
		}
		i++
	}
}

// newMetacache constructs a new metacache from the options.
func (o listPathOptions) newMetacache() metacache {
	return metacache{
		id:           o.ID,
		bucket:       o.Bucket,
		root:         o.BaseDir,
		recursive:    o.Recursive,
		status:       scanStateStarted,
		error:        "",
		started:      UTCNow(),
		lastHandout:  UTCNow(),
		lastUpdate:   UTCNow(),
		ended:        time.Time{},
		startedCycle: o.CurrentCycle,
		endedCycle:   0,
		dataVersion:  metacacheStreamVersion,
	}
}

func getMetacacheBlockInfo(fi FileInfo, block int) (*metacacheBlock, error) {
	var tmp metacacheBlock
	partKey := fmt.Sprintf("%s-metacache-part-%d", ReservedMetadataPrefixLower, block)
	v, ok := fi.Metadata[partKey]
	if !ok {
		return nil, io.ErrUnexpectedEOF
	}
	return &tmp, json.Unmarshal([]byte(v), &tmp)
}

func metacachePrefixForID(bucket, id string) string {
	return pathJoin("buckets", bucket, ".metacache", id)
}

// objectPath returns the object path of the cache.
func (o *listPathOptions) objectPath(block int) string {
	return pathJoin(metacachePrefixForID(o.Bucket, o.ID), "block-"+strconv.Itoa(block)+".s2")
}

// filter will apply the options and return the number of objects requested by the limit.
// Will return io.EOF if there are no more entries with the same filter.
// The last entry can be used as a marker to resume the listing.
func (r *metacacheReader) filter(o listPathOptions) (entries metaCacheEntriesSorted, err error) {
	const debugPrint = false
	// Forward to prefix, if any
	err = r.forwardTo(o.Prefix)
	if err != nil {
		return entries, err
	}
	if o.Marker != "" {
		err = r.forwardTo(o.Marker)
		if err != nil {
			return entries, err
		}
		next, err := r.peek()
		if err != nil {
			return entries, err
		}
		if next.name == o.Marker {
			err := r.skip(1)
			if err != nil {
				return entries, err
			}
		}
	}
	if debugPrint {
		console.Infoln("forwarded to ", o.Prefix, "marker:", o.Marker, "sep:", o.Separator)
	}
	// Filter
	if !o.Recursive {
		entries.o = make(metaCacheEntries, 0, o.Limit)
		pastPrefix := false
		err := r.readFn(func(entry metaCacheEntry) bool {
			if o.Prefix != "" && !strings.HasPrefix(entry.name, o.Prefix) {
				// We are past the prefix, don't continue.
				pastPrefix = true
				return false
			}
			if !o.IncludeDirectories && entry.isDir() {
				return true
			}
			if !entry.isInDir(o.Prefix, o.Separator) {
				return true
			}
			if !o.InclDeleted && entry.isObject() && entry.isLatestDeletemarker() {
				return entries.len() < o.Limit
			}
			entries.o = append(entries.o, entry)
			return entries.len() < o.Limit
		})
		if (err != nil && err.Error() == io.EOF.Error()) || pastPrefix || r.nextEOF() {
			return entries, io.EOF
		}
		return entries, err
	}

	// We should not need to filter more.
	return r.readN(o.Limit, o.InclDeleted, o.IncludeDirectories, o.Prefix)
}

func (er *erasureObjects) streamMetadataParts(ctx context.Context, o listPathOptions) (entries metaCacheEntriesSorted, err error) {
	retries := 0
	const debugPrint = false
	for {
		select {
		case <-ctx.Done():
			return entries, ctx.Err()
		default:
		}

		// Load first part metadata...
		// All operations are performed without locks, so we must be careful and allow for failures.
		fi, metaArr, onlineDisks, err := er.getObjectFileInfo(ctx, minioMetaBucket, o.objectPath(0), ObjectOptions{})
		if err != nil {
			if err == errFileNotFound || errors.Is(err, errErasureReadQuorum) || errors.Is(err, InsufficientReadQuorum{}) {
				// Not ready yet...
				if retries == 10 {
					err := o.checkMetacacheState(ctx)
					if debugPrint {
						logger.Info("waiting for first part (%s), err: %v", o.objectPath(0), err)
					}
					if err != nil {
						return entries, err
					}
					retries = 0
					continue
				}
				retries++
				time.Sleep(100 * time.Millisecond)
				continue
			}
			if debugPrint {
				console.Infoln("first getObjectFileInfo", o.objectPath(0), "returned err:", err)
				console.Infof("err type: %T\n", err)
			}
			return entries, err
		}
		if fi.Deleted {
			return entries, errFileNotFound
		}

		partN, err := o.findFirstPart(fi)
		switch err {
		case nil:
		case io.ErrUnexpectedEOF, errErasureReadQuorum, InsufficientReadQuorum{}:
			if retries == 10 {
				err := o.checkMetacacheState(ctx)
				if debugPrint {
					logger.Info("waiting for metadata, err: %v", err)
				}
				if err != nil {
					return entries, err
				}
				retries = 0
				continue
			}
			retries++
			time.Sleep(100 * time.Millisecond)
			continue
		case io.EOF:
			return entries, io.EOF
		}
		// We got a stream to start at.
		loadedPart := 0
		var buf bytes.Buffer
		for {
			select {
			case <-ctx.Done():
				return entries, ctx.Err()
			default:
			}

			if partN != loadedPart {
				// Load first part metadata...
				fi, metaArr, onlineDisks, err = er.getObjectFileInfo(ctx, minioMetaBucket, o.objectPath(partN), ObjectOptions{})
				switch err {
				case errFileNotFound, errErasureReadQuorum, InsufficientReadQuorum{}:
					if retries >= 10 {
						err := o.checkMetacacheState(ctx)
						if debugPrint {
							logger.Info("waiting for part data (%v), err: %v", o.objectPath(partN), err)
						}
						if err != nil {
							return entries, err
						}
						retries = 0
						continue
					}
					time.Sleep(100 * time.Millisecond)
					continue
				default:
					time.Sleep(100 * time.Millisecond)
					if retries >= 20 {
						// We had at least 10 retries without getting a result.
						logger.LogIf(ctx, err)
						return entries, err
					}
					retries++
					continue
				case nil:
					loadedPart = partN
					bi, err := getMetacacheBlockInfo(fi, partN)
					logger.LogIf(ctx, err)
					if err == nil {
						if bi.pastPrefix(o.Prefix) {
							return entries, io.EOF
						}
					}
				}
				if fi.Deleted {
					return entries, io.ErrUnexpectedEOF
				}
			}
			buf.Reset()
			err := er.getObjectWithFileInfo(ctx, minioMetaBucket, o.objectPath(partN), 0, fi.Size, &buf, fi, metaArr, onlineDisks)
			switch err {
			case errFileNotFound, errErasureReadQuorum, InsufficientReadQuorum{}:
				if retries >= 20 {
					// We had at least 10 retries without getting a result.
					logger.LogIf(ctx, err)
					return entries, err
				}
				retries++
				time.Sleep(100 * time.Millisecond)
				continue
			default:
				logger.LogIf(ctx, err)
				return entries, err
			case nil:
			}
			tmp, err := newMetacacheReader(&buf)
			if err != nil {
				return entries, err
			}
			e, err := tmp.filter(o)
			entries.o = append(entries.o, e.o...)
			if o.Limit > 0 && entries.len() > o.Limit {
				entries.truncate(o.Limit)
				return entries, nil
			}
			switch err {
			case io.EOF:
				// We finished at the end of the block.
				// And should not expect any more results.
				bi, err := getMetacacheBlockInfo(fi, partN)
				logger.LogIf(ctx, err)
				if err != nil || bi.EOS {
					// We are done and there are no more parts.
					return entries, io.EOF
				}
				if bi.endedPrefix(o.Prefix) {
					// Nothing more for prefix.
					return entries, io.EOF
				}
				partN++
			case nil:
				// We stopped within the listing, we are done for now...
				return entries, nil
			default:
				return entries, err
			}
		}
	}
}

// Will return io.EOF if continuing would not yield more results.
func (er *erasureObjects) listPath(ctx context.Context, o listPathOptions) (entries metaCacheEntriesSorted, err error) {
	const debugPrint = false
	if debugPrint {
		console.Printf("listPath with options: %#v", o)
	}
	// See if we have the listing stored.
	if !o.Create {
		entries, err := er.streamMetadataParts(ctx, o)
		switch err {
		case nil, io.EOF, context.Canceled, context.DeadlineExceeded:
			return entries, err
		}
		logger.LogIf(ctx, err)
		return entries, err
	}

	rpcClient := globalNotificationSys.restClientFromHash(o.Bucket)
	meta := o.newMetacache()
	var metaMu sync.Mutex
	defer func() {
		if debugPrint {
			console.Println("listPath returning:", entries.len(), "err:", err)
		}
		if err != nil {
			metaMu.Lock()
			if meta.status != scanStateError {
				meta.error = err.Error()
				meta.status = scanStateError
			}
			lm := meta
			metaMu.Unlock()
			if rpcClient == nil {
				localMetacacheMgr.getBucket(GlobalContext, o.Bucket).updateCacheEntry(lm)
			} else {
				rpcClient.UpdateMetacacheListing(context.Background(), lm)
			}
		}
	}()
	if debugPrint {
		console.Println("listPath: scanning bucket:", o.Bucket, "basedir:", o.BaseDir, "prefix:", o.Prefix, "marker:", o.Marker)
	}

	// Disconnect from call above, but cancel on exit.
	ctx, cancel := context.WithCancel(GlobalContext)
	// We need to ask disks.
	disks := er.getOnlineDisks()

	var askDisks = o.AskDisks
	switch askDisks {
	// 50% or at least 3.
	case -1:
		o.AskDisks = getReadQuorum(len(er.getDisks()))
		if o.AskDisks < 3 {
			o.AskDisks = 3
		}
	// Default is 3 disks.
	case 0:
		askDisks = 3
	}

	if len(disks) < askDisks {
		err = InsufficientReadQuorum{}
		if debugPrint {
			console.Errorf("listPath: Insufficient disks, %d of %d needed are available", len(disks), askDisks)
		}
		logger.LogIf(ctx, fmt.Errorf("listPath: Insufficient disks, %d of %d needed are available", len(disks), askDisks))
		cancel()
		return
	}

	// Select askDisks random disks, 3 is ok.
	if len(disks) > askDisks {
		disks = disks[:askDisks]
	}
	var readers = make([]*metacacheReader, askDisks)
	for i := range disks {
		r, w := io.Pipe()
		d := disks[i]
		readers[i], err = newMetacacheReader(r)
		if err != nil {
			cancel()
			return entries, err
		}
		// Send request.
		go func() {
			err := d.WalkDir(ctx, WalkDirOptions{Bucket: o.Bucket, BaseDir: o.BaseDir, Recursive: o.Recursive || o.Separator != SlashSeparator}, w)
			w.CloseWithError(err)
			if err != io.EOF {
				logger.LogIf(ctx, err)
			}
		}()
	}

	// Create output for our results.
	cacheCh := make(chan metaCacheEntry, metacacheBlockSize)

	// Create filter for results.
	filterCh := make(chan metaCacheEntry, 100)
	filteredResults := o.gatherResults(filterCh)
	closeChannels := func() {
		close(cacheCh)
		close(filterCh)
	}

	go func() {
		defer cancel()
		// Save continuous updates
		go func() {
			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()
			var exit bool
			for !exit {
				select {
				case <-ticker.C:
				case <-ctx.Done():
					exit = true
				}
				metaMu.Lock()
				meta.endedCycle = intDataUpdateTracker.current()
				lm := meta
				metaMu.Unlock()
				var err error
				if o.Transient {
					lm, err = localMetacacheMgr.getTransient().updateCacheEntry(lm)
				} else if rpcClient == nil {
					lm, err = localMetacacheMgr.getBucket(GlobalContext, o.Bucket).updateCacheEntry(lm)
				} else {
					lm, err = rpcClient.UpdateMetacacheListing(context.Background(), lm)
				}
				logger.LogIf(ctx, err)
				if lm.status == scanStateError {
					cancel()
					exit = true
				}
			}
		}()

		const retryDelay = 200 * time.Millisecond
		const maxTries = 10

		// Write results to disk.
		bw := newMetacacheBlockWriter(cacheCh, func(b *metacacheBlock) error {
			if debugPrint {
				console.Println("listPath: saving block", b.n, "to", o.objectPath(b.n))
			}
			r, err := hash.NewReader(bytes.NewBuffer(b.data), int64(len(b.data)), "", "", int64(len(b.data)), false)
			logger.LogIf(ctx, err)
			custom := b.headerKV()
			custom[xhttp.AmzStorageClass] = storageclass.RRS
			_, err = er.putObject(ctx, minioMetaBucket, o.objectPath(b.n), NewPutObjReader(r, nil, nil), ObjectOptions{UserDefined: custom})
			if err != nil {
				metaMu.Lock()
				meta.status = scanStateError
				meta.error = err.Error()
				metaMu.Unlock()
				cancel()
				return err
			}
			if b.n == 0 {
				return nil
			}
			// Update block 0 metadata.
			var retries int
			for {
				err := er.updateObjectMeta(ctx, minioMetaBucket, o.objectPath(0), b.headerKV(), ObjectOptions{})
				if err == nil {
					break
				}
				switch err.(type) {
				case ObjectNotFound:
					return err
				case InsufficientReadQuorum:
				default:
					logger.LogIf(ctx, err)
				}
				if retries >= maxTries {
					return err
				}
				retries++
				time.Sleep(retryDelay)
			}
			return nil
		})

		// How to resolve results.
		resolver := metadataResolutionParams{
			dirQuorum: askDisks - 1,
			objQuorum: askDisks - 1,
			bucket:    o.Bucket,
		}

		topEntries := make(metaCacheEntries, len(readers))
		for {
			// Get the top entry from each
			var current metaCacheEntry
			var atEOF, agree int
			for i, r := range readers {
				topEntries[i].name = ""
				entry, err := r.peek()
				switch err {
				case io.EOF:
					atEOF++
					continue
				case nil:
				default:
					closeChannels()
					metaMu.Lock()
					meta.status = scanStateError
					meta.error = err.Error()
					metaMu.Unlock()
					return
				}
				// If no current, add it.
				if current.name == "" {
					topEntries[i] = entry
					current = entry
					agree++
					continue
				}
				// If exact match, we agree.
				if current.matches(&entry, o.Bucket) {
					topEntries[i] = entry
					agree++
					continue
				}
				// If only the name matches we didn't agree, but add it for resolution.
				if entry.name == current.name {
					topEntries[i] = entry
					continue
				}
				// We got different entries
				if entry.name > current.name {
					continue
				}
				// We got a new, better current.
				// Clear existing entries.
				for i := range topEntries[:i] {
					topEntries[i] = metaCacheEntry{}
				}
				agree = 1
				current = entry
				topEntries[i] = entry
			}
			// Break if all at EOF.
			if atEOF == len(readers) {
				break
			}
			if agree == len(readers) {
				// Everybody agreed
				for _, r := range readers {
					r.skip(1)
				}
				cacheCh <- topEntries[0]
				filterCh <- topEntries[0]
				continue
			}

			// Results Disagree :-(
			entry, ok := topEntries.resolve(&resolver)
			if ok {
				cacheCh <- *entry
				filterCh <- *entry
			}
			// Skip the inputs we used.
			for i, r := range readers {
				if topEntries[i].name != "" {
					r.skip(1)
				}
			}
		}
		closeChannels()
		metaMu.Lock()
		if meta.error == "" {
			if err := bw.Close(); err != nil {
				meta.error = err.Error()
				meta.status = scanStateError
			} else {
				meta.status = scanStateSuccess
				meta.endedCycle = intDataUpdateTracker.current()
			}
		}
		metaMu.Unlock()
	}()

	return filteredResults()
}
