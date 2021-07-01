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

package target

import (
	"encoding/json"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/minio/minio/internal/event"
)

const (
	defaultLimit = 100000 // Default store limit.
	eventExt     = ".event"
)

// QueueStore - Filestore for persisting events.
type QueueStore struct {
	sync.RWMutex
	currentEntries uint64
	entryLimit     uint64
	directory      string
}

// NewQueueStore - Creates an instance for QueueStore.
func NewQueueStore(directory string, limit uint64) Store {
	if limit == 0 {
		limit = defaultLimit
	}

	return &QueueStore{
		directory:  directory,
		entryLimit: limit,
	}
}

// Open - Creates the directory if not present.
func (store *QueueStore) Open() error {
	store.Lock()
	defer store.Unlock()

	if err := os.MkdirAll(store.directory, os.FileMode(0770)); err != nil {
		return err
	}

	names, err := store.list()
	if err != nil {
		return err
	}

	currentEntries := uint64(len(names))
	if currentEntries >= store.entryLimit {
		return errLimitExceeded
	}

	store.currentEntries = currentEntries

	return nil
}

// write - writes event to the directory.
func (store *QueueStore) write(key string, e event.Event) error {

	// Marshalls the event.
	eventData, err := json.Marshal(e)
	if err != nil {
		return err
	}

	path := filepath.Join(store.directory, key+eventExt)
	if err := ioutil.WriteFile(path, eventData, os.FileMode(0770)); err != nil {
		return err
	}

	// Increment the event count.
	store.currentEntries++

	return nil
}

// Put - puts a event to the store.
func (store *QueueStore) Put(e event.Event) error {
	store.Lock()
	defer store.Unlock()
	if store.currentEntries >= store.entryLimit {
		return errLimitExceeded
	}
	key, err := getNewUUID()
	if err != nil {
		return err
	}
	return store.write(key, e)
}

// Get - gets a event from the store.
func (store *QueueStore) Get(key string) (event event.Event, err error) {
	store.RLock()

	defer func(store *QueueStore) {
		store.RUnlock()
		if err != nil {
			// Upon error we remove the entry.
			store.Del(key)
		}
	}(store)

	var eventData []byte
	eventData, err = ioutil.ReadFile(filepath.Join(store.directory, key+eventExt))
	if err != nil {
		return event, err
	}

	if len(eventData) == 0 {
		return event, os.ErrNotExist
	}

	if err = json.Unmarshal(eventData, &event); err != nil {
		return event, err
	}

	return event, nil
}

// Del - Deletes an entry from the store.
func (store *QueueStore) Del(key string) error {
	store.Lock()
	defer store.Unlock()
	return store.del(key)
}

// lockless call
func (store *QueueStore) del(key string) error {
	if err := os.Remove(filepath.Join(store.directory, key+eventExt)); err != nil {
		return err
	}

	// Decrement the current entries count.
	store.currentEntries--

	// Current entries can underflow, when multiple
	// events are being pushed in parallel, this code
	// is needed to ensure that we don't underflow.
	//
	// queueStore replayEvents is not serialized,
	// this code is needed to protect us under
	// such situations.
	if store.currentEntries == math.MaxUint64 {
		store.currentEntries = 0
	}
	return nil
}

// List - lists all files from the directory.
func (store *QueueStore) List() ([]string, error) {
	store.RLock()
	defer store.RUnlock()
	return store.list()
}

// list lock less.
func (store *QueueStore) list() ([]string, error) {
	var names []string
	files, err := ioutil.ReadDir(store.directory)
	if err != nil {
		return names, err
	}

	// Sort the dentries.
	sort.Slice(files, func(i, j int) bool {
		return files[i].ModTime().Before(files[j].ModTime())
	})

	for _, file := range files {
		names = append(names, file.Name())
	}

	return names, nil
}
