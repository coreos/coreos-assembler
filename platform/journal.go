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
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/coreos/pkg/multierror"

	"github.com/coreos/mantle/network/journal"
	"github.com/coreos/mantle/util"
)

// Journal manages recording the journal of a Machine.
type Journal struct {
	journal    io.WriteCloser
	journalRaw io.WriteCloser
	recorder   *journal.Recorder
	cancel     context.CancelFunc
}

// NewJournal creates a Journal recorder that will log to "journal.txt"
// and "journal-raw.txt.gz" inside the given output directory.
func NewJournal(dir string) (*Journal, error) {
	p := filepath.Join(dir, "journal.txt")
	j, err := os.OpenFile(p, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0666)
	if err != nil {
		return nil, err
	}

	p = filepath.Join(dir, "journal-raw.txt.gz")
	jr, err := os.OpenFile(p, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0666)
	if err != nil {
		return nil, err
	}
	// gzip to save space; a single test can generate well over 1M of logs
	jrz, err := gzip.NewWriterLevel(jr, gzip.BestCompression)
	if err != nil {
		return nil, err
	}

	return &Journal{
		journal:    j,
		journalRaw: jrz,
		recorder:   journal.NewRecorder(journal.ShortWriter(j), jrz),
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
	var err multierror.Error
	if j.cancel != nil {
		j.cancel()
		if e := j.recorder.Wait(); e != nil {
			err = append(err, e)
		}
	}
	if e := j.journal.Close(); e != nil {
		err = append(err, e)
	}
	if e := j.journalRaw.Close(); e != nil {
		err = append(err, e)
	}
	return err.AsError()
}
