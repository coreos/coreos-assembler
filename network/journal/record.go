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

package journal

import (
	"context"
	"io"
	"os"

	"github.com/kballard/go-shellquote"
	"golang.org/x/crypto/ssh"

	"github.com/coreos/mantle/system/exec"
)

type Recorder struct {
	formatter Formatter
	cursor    string
	status    chan error
	rawFile   io.WriteCloser
}

func NewRecorder(f Formatter, rawFile io.WriteCloser) *Recorder {
	return &Recorder{
		formatter: f,
		rawFile:   rawFile,
		status:    make(chan error, 1),
	}
}

func (r *Recorder) journalctl() []string {
	cmd := []string{"journalctl",
		"--output=export", "--follow", "--lines=all"}
	if r.cursor == "" {
		cmd = append(cmd, "--boot")
	} else {
		cmd = append(cmd, "--after-cursor", r.cursor)
	}
	return cmd
}

func (r *Recorder) record(export io.Reader) error {
	exportTee := io.TeeReader(export, r.rawFile)
	src := NewExportReader(exportTee)
	for {
		entry, err := src.ReadEntry()
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			return err
		}

		r.cursor = string(entry[FIELD_CURSOR])

		if err := r.formatter.WriteEntry(entry); err != nil {
			return err
		}
	}
}

func (r *Recorder) StartSSH(ctx context.Context, client *ssh.Client) error {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		<-ctx.Done()
		client.Close()
	}()

	journal, err := client.NewSession()
	if err != nil {
		cancel()
		return err
	}
	journal.Stderr = os.Stderr

	export, err := journal.StdoutPipe()
	if err != nil {
		cancel()
		return err
	}

	cmd := shellquote.Join(r.journalctl()...)
	if err := journal.Start(cmd); err != nil {
		cancel()
		return err
	}

	go func() {
		err := r.record(export)
		cancel()
		err2 := journal.Wait()
		// Tolerate closed/canceled SSH connections.
		if _, ok := err2.(*ssh.ExitMissingError); ok {
			err2 = nil
		}
		if err == nil && err2 != nil {
			err = err2
		}
		r.status <- err
	}()

	return nil
}

func (r *Recorder) StartLocal(ctx context.Context) error {
	cmd := r.journalctl()
	journal := exec.CommandContext(ctx, cmd[0], cmd[1:]...)
	journal.Stderr = os.Stderr

	export, err := journal.StdoutPipe()
	if err != nil {
		return err
	}

	if err := journal.Start(); err != nil {
		return err
	}

	go func() {
		err := r.record(export)
		err2 := journal.Wait()
		if err == nil && err2 != nil {
			err = err2
		}
		r.status <- err
	}()

	return nil
}

func (r *Recorder) Wait() error {
	return <-r.status
}

func (r *Recorder) RunSSH(ctx context.Context, client *ssh.Client) error {
	if err := r.StartSSH(ctx, client); err != nil {
		return err
	}
	return r.Wait()
}

func (r *Recorder) RunLocal(ctx context.Context) error {
	if err := r.StartLocal(ctx); err != nil {
		return err
	}
	return r.Wait()
}
