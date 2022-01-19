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
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/coreos/pkg/multierror"
	"github.com/pkg/errors"

	"github.com/coreos/mantle/network/journal"
	"github.com/coreos/mantle/util"
)

// Journal manages recording the journal of a Machine.
type Journal struct {
	journal     io.WriteCloser
	journalRaw  io.WriteCloser
	journalPath string
	recorder    *journal.Recorder
	cancel      context.CancelFunc
}

// wrapper that also closes the underlying file
type gzWriteCloser struct {
	*gzip.Writer
	underlying io.Closer
}

func (g gzWriteCloser) Close() error {
	var err multierror.Error
	if e := g.Writer.Close(); e != nil {
		err = append(err, e)
	}
	if e := g.underlying.Close(); e != nil {
		err = append(err, e)
	}
	return err.AsError()
}

// NewJournal creates a Journal recorder that will log to "journal.txt"
// and "journal-raw.txt.gz" inside the given output directory.
func NewJournal(dir string) (*Journal, error) {
	p := filepath.Join(dir, "journal.txt")
	j, err := os.OpenFile(p, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0666)
	if err != nil {
		return nil, err
	}

	pr := filepath.Join(dir, "journal-raw.txt.gz")
	jr, err := os.OpenFile(pr, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0666)
	if err != nil {
		return nil, err
	}
	// gzip to save space; a single test can generate well over 1M of logs
	jrz, err := gzip.NewWriterLevel(jr, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	jrzc := gzWriteCloser{
		underlying: jr,
		Writer:     jrz,
	}

	return &Journal{
		journal:     j,
		journalRaw:  jrzc,
		recorder:    journal.NewRecorder(journal.ShortWriter(j), jrzc),
		journalPath: p,
	}, nil
}

// Start begins/resumes streaming the system journal to journal.txt.
func (j *Journal) Start(ctx context.Context, m Machine, oldBootId string) error {
	if j.cancel != nil {
		j.cancel()
		j.cancel = nil
		_ = j.recorder.Wait() // Just need to consume the status.
	}
	ctx, cancel := context.WithCancel(ctx)

	start := func() error {
		if oldBootId != "" {
			bootId, err := GetMachineBootId(m)
			if err != nil {
				return err
			} else if bootId == oldBootId {
				return fmt.Errorf("found old boot ID %s (likely still rebooting)", oldBootId)
			}
		}

		client, err := m.SSHClient()
		if err != nil {
			return err
		}

		return j.recorder.StartSSH(ctx, client)
	}

	// Retry for a while because the machine is likely still booting
	// and some Ignition configs take a long time to apply.
	if err := util.RetryUntilTimeout(10*time.Minute, 10*time.Second, start); err != nil {
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
	return ioutil.ReadAll(f)
}

func (j *Journal) Destroy() {
	if j.cancel != nil {
		j.cancel()
		if err := j.recorder.Wait(); err != nil {
			plog.Errorf("j.recorder.Wait() failed: %v", err)
		}
	}
	if err := j.journal.Close(); err != nil {
		plog.Errorf("Failed to close journal: %v", err)
	}
	if err := j.journalRaw.Close(); err != nil {
		plog.Errorf("Failed to close raw journal: %v", err)
	}
}
