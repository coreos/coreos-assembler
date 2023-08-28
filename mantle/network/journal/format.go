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
	"bytes"
	"fmt"
	"io"
	"time"
	"unicode"
	"unicode/utf8"
)

type Formatter interface {
	SetTimezone(tz *time.Location)
	WriteEntry(entry Entry) error
}

type shortWriter struct {
	w      io.Writer
	tz     *time.Location
	bootid string
}

// ShortWriter writes journal entries in a format similar to journalctl's
// "short-precise" format, excluding hostname for conciseness.
func ShortWriter(w io.Writer) Formatter {
	return &shortWriter{
		w:  w,
		tz: time.Local,
	}
}

// SetTimezone updates the time location. The default is local time.
func (s *shortWriter) SetTimezone(tz *time.Location) {
	s.tz = tz
}

func (s *shortWriter) WriteEntry(entry Entry) error {
	realtime := entry.Realtime()
	message, ok := entry[FIELD_MESSAGE]
	if realtime.IsZero() || !ok {
		// Simply skip entries that are woefully incomplete.
		return nil
	}

	if s.isReboot(entry) {
		if _, err := io.WriteString(s.w, "-- Reboot --\n"); err != nil {
			return err
		}
	}

	var buf bytes.Buffer
	buf.WriteString(realtime.In(s.tz).Format(time.StampMicro))

	// Default to equivalent of journalctl -o with-unit, because its value is
	// trusted, and the syslog identifier (commonly when executing bash via ExecStart)
	// can be garbage.
	if unit, ok := entry[FIELD_SYSTEMD_UNIT]; ok {
		buf.WriteByte(' ')
		buf.WriteString(string(unit))
	} else if identifier, ok := entry[FIELD_SYSLOG_IDENTIFIER]; ok {
		buf.WriteByte(' ')
		buf.Write(identifier)
	} else {
		buf.WriteString(" unknown")
	}

	if pid, ok := entry[FIELD_PID]; ok {
		buf.WriteByte('[')
		buf.Write(pid)
		buf.WriteByte(']')
	} else if pid, ok := entry[FIELD_SYSLOG_PID]; ok {
		buf.WriteByte('[')
		buf.Write(pid)
		buf.WriteByte(']')
	}

	buf.WriteString(": ")
	indent := buf.Len()
	lines := bytes.Split(message, []byte{'\n'})
	writeEscaped(&buf, lines[0])
	for _, line := range lines[1:] {
		buf.WriteByte('\n')
		buf.Write(bytes.Repeat([]byte{' '}, indent))
		writeEscaped(&buf, line)
	}

	buf.WriteByte('\n')

	_, err := buf.WriteTo(s.w)
	return err
}

func (s *shortWriter) isReboot(entry Entry) bool {
	bootid, ok := entry[FIELD_BOOT_ID]
	if !ok || len(bootid) == 0 {
		return false
	}

	newid := string(bootid)
	if s.bootid == "" {
		s.bootid = newid
		return false
	} else if s.bootid != newid {
		s.bootid = newid
		return true
	}
	return false
}

func writeEscaped(buf *bytes.Buffer, line []byte) {
	const tab = "        " // 8 spaces
	for len(line) > 0 {
		r, n := utf8.DecodeRune(line)
		switch r {
		case utf8.RuneError:
			fmt.Fprintf(buf, "\\x%02x", line[0])
		case '\t':
			buf.WriteString(tab)
		default:
			if unicode.IsPrint(r) {
				buf.Write(line[:n])
			} else {
				fmt.Fprintf(buf, "\\u%04x", r)
			}
		}
		line = line[n:]
	}
}
