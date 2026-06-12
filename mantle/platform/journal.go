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
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/pkg/errors"

	"github.com/coreos/coreos-assembler/mantle/network/journal"
	"github.com/coreos/coreos-assembler/mantle/util"
)

// Journal manages recording the journal of a Machine.
type Journal struct {
	// journalInputPipe, when non-nil, indicates journal recording is
	// handled via a virtio pipe rather than SSH. Once set and started,
	// the recorder goroutine persists across guest reboots.
	journalInputPipe io.ReadCloser
	journal          io.WriteCloser
	journalPath      string
	recorder         *journal.Recorder
	cancel           context.CancelFunc
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
		journal:     j,
		recorder:    journal.NewRecorder(journal.ShortWriter(j)),
		journalPath: p,
	}, nil
}

// StartVirtioJournal begins recording the journal from a virtio pipe.
// The pipe persists across guest reboots, so this only needs to be
// called once. Subsequent calls to Start() will be no-ops.
func (j *Journal) StartVirtioJournal(pipe io.ReadCloser) error {
	j.journalInputPipe = pipe
	return j.recorder.StartFile(pipe)
}

// Start begins/resumes streaming the system journal to journal.txt.
func (j *Journal) Start(m Machine) error {
	// If a virtio pipe journal is active, it persists across reboots
	// and there is nothing to do.
	if j.journalInputPipe != nil {
		return nil
	}

	if j.cancel != nil {
		j.cancel()
		j.cancel = nil
		_ = j.recorder.Wait() // Just need to consume the status.
	}
	ctx := m.RuntimeConf().TestExecTimeout
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)

	start := func() error {
		client, err := m.SSHClient()
		if err != nil {
			return err
		}

		return j.recorder.StartSSH(ctx, client)
	}

	// Retry for a while because the machine is likely still booting
	// and some Ignition configs take a long time to apply.
	if err := util.RetryUntilTimeoutWithContext(ctx, 10*time.Minute, 10*time.Second, start); err != nil {
		cancel()
		return errors.Wrapf(err, "ssh journalctl failed")
	}

	j.cancel = cancel
	return nil
}

// There is no guarantee that anything is returned if called before Destroy
func (j *Journal) Read() ([]byte, error) {
	f, err := os.Open(j.journalPath)
	if err != nil {
		return nil, errors.Wrapf(err, "reading journal")
	}
	defer f.Close()
	return io.ReadAll(f)
}

func (j *Journal) Destroy() {
	if j.cancel != nil {
		j.cancel()
		if err := j.recorder.Wait(); err != nil {
			plog.Errorf("j.recorder.Wait() failed: %v", err)
		}
	}
	if j.journalInputPipe != nil {
		if err := j.journalInputPipe.Close(); err != nil {
			plog.Errorf("Failed to close journal input pipe: %v", err)
		}
		// Wait for the recorder goroutine to exit after closing the virtio
		// journal input pipe. Without this, the goroutine may still be writing
		// to j.journal when j.journal.Close() is called.
		if err := j.recorder.Wait(); err != nil {
			plog.Errorf("j.recorder.Wait() failed: %v", err)
		}
	}
	if err := j.journal.Close(); err != nil {
		plog.Errorf("Failed to close journal: %v", err)
	}
}
