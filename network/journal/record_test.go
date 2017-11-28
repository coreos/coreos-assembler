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
	"io/ioutil"
	"strings"
	"testing"
	"time"

	"github.com/coreos/mantle/network/mockssh"
)

const (
	// cursor strings for exportText and exportBinary from export_test.go
	cursorText   = "s=739ad463348b4ceca5a9e69c95a3c93f;i=4ece8;b=6c7c6013a26343b29e964691ff25d04c;m=4fc72572f;t=4c508a7243799;x=68597058a89b7246;p=system.journal"
	cursorBinary = "s=bcce4fb8ffcb40e9a6e05eee8b7831bf;i=5ef603;b=ec25d6795f0645619ddac9afdef453ee;m=545242e7049;t=50f1202"

	// commands the recorder should execute
	journalBoot  = "journalctl --output=export --follow --lines=all --boot"
	journalAfter = "journalctl --output=export --follow --lines=all --after-cursor " // + cursorText or cursorBinary
)

type nullFormatter struct{}

func (n nullFormatter) SetTimezone(tz *time.Location) {}

func (n nullFormatter) WriteEntry(entry Entry) error {
	return nil
}

type discardCloser struct{}

func (d discardCloser) Close() error {
	return nil
}

func (d discardCloser) Write(b []byte) (int, error) {
	return ioutil.Discard.Write(b)
}

// Escapes ; chars in cursor with \
// May need tweaking if the shellquote library behavior ever changes.
func journalAfterEsc(cursor string) string {
	return journalAfter + strings.Replace(cursor, ";", "\\;", -1)
}

func TestRecorderSSH(t *testing.T) {
	ctx := context.Background()
	client := mockssh.NewMockClient(func(s *mockssh.Session) {
		if s.Exec != journalBoot {
			t.Errorf("got %q wanted %q", s.Exec, journalBoot)
		}
		if _, err := io.WriteString(s.Stdout, exportText); err != nil {
			t.Error(err)
		}
		if err := s.Exit(0); err != nil {
			t.Error(err)
		}
	})

	recorder := NewRecorder(nullFormatter{}, discardCloser{})
	if err := recorder.RunSSH(ctx, client); err != nil {
		t.Fatal(err)
	}

	if recorder.cursor != cursorText {
		t.Errorf("got %q wanted %q", recorder.cursor, cursorText)
	}

	client = mockssh.NewMockClient(func(s *mockssh.Session) {
		cmd := journalAfterEsc(cursorText)
		if s.Exec != cmd {
			t.Errorf("got %q wanted %q", s.Exec, cmd)
		}
		if _, err := io.WriteString(s.Stdout, exportBinary); err != nil {
			t.Error(err)
		}
		if err := s.Exit(0); err != nil {
			t.Error(err)
		}
	})

	if err := recorder.RunSSH(ctx, client); err != nil {
		t.Fatal(err)
	}

	if recorder.cursor != cursorBinary {
		t.Errorf("got %q wanted %q", recorder.cursor, cursorBinary)
	}

	client = mockssh.NewMockClient(func(s *mockssh.Session) {
		cmd := journalAfterEsc(cursorBinary)
		if s.Exec != cmd {
			t.Errorf("got %q wanted %q", s.Exec, cmd)
		}
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})

	if err := recorder.RunSSH(ctx, client); err != nil {
		t.Fatal(err)
	}

	if recorder.cursor != cursorBinary {
		t.Errorf("got %q wanted %q", recorder.cursor, cursorBinary)
	}
}

func TestRecorderSSHCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := mockssh.NewMockClient(func(s *mockssh.Session) {
		// Skip close, let the connection hang.
	})

	recorder := NewRecorder(nullFormatter{}, discardCloser{})
	if err := recorder.StartSSH(ctx, client); err != nil {
		t.Fatal(err)
	}

	cancel()

	if err := recorder.Wait(); err != nil {
		t.Fatal(err)
	}
}
