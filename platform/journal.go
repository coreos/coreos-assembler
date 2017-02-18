// Copyright 2017 CoreOS, Inc.
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

package platform

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/coreos/mantle/network/journal"
	"github.com/coreos/mantle/util"
)

// Journal manages recording the journal of a Machine.
type Journal struct {
	journal  *os.File
	recorder *journal.Recorder
	cancel   context.CancelFunc
}

// NewJournal creates a Journal recorder that will log to "journal.txt"
// inside the given output directory.
func NewJournal(dir string) (*Journal, error) {
	p := filepath.Join(dir, "journal.txt")
	j, err := os.OpenFile(p, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0666)
	if err != nil {
		return nil, err
	}

	return &Journal{
		journal:  j,
		recorder: journal.NewRecorder(journal.ShortWriter(j)),
	}, nil
}

// Start begins/resumes streaming the system journal to journal.txt.
func (j *Journal) Start(ctx context.Context, m Machine) error {
	if j.cancel != nil {
		j.cancel()
		j.cancel = nil
		j.recorder.Wait() // Just need to consume the status.
	}
	ctx, cancel := context.WithCancel(ctx)

	start := func() error {
		client, err := m.SSHClient()
		if err != nil {
			return err
		}

		return j.recorder.StartSSH(ctx, client)
	}

	// Retry for a while because this should be run before CheckMachine
	if err := util.Retry(sshRetries, sshTimeout, start); err != nil {
		cancel()
		return fmt.Errorf("ssh journalctl failed: %v", err)
	}

	j.cancel = cancel
	return nil
}

func (j *Journal) Destroy() error {
	var err error
	if j.cancel != nil {
		j.cancel()
		err = j.recorder.Wait()
	}
	if err2 := j.journal.Close(); err == nil && err2 != nil {
		err = err2
	}
	return err
}
