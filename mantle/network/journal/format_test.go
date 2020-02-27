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
	"io"
	"strings"
	"testing"
	"time"

	"github.com/kylelemons/godebug/diff"
)

func TestFormatShort(t *testing.T) {
	for _, testcase := range []struct {
		name   string
		entry  Entry
		expect string
	}{{
		name: "all",
		entry: Entry{
			FIELD_REALTIME_TIMESTAMP:        []byte("1342540861421465"),
			FIELD_SOURCE_REALTIME_TIMESTAMP: []byte("1342540861416351"),
			FIELD_SYSLOG_IDENTIFIER:         []byte("/USR/SBIN/CROND"),
			FIELD_SYSLOG_PID:                []byte("8278"),
			FIELD_PID:                       []byte("8278"),
			FIELD_MESSAGE:                   []byte("(root) CMD (run-parts /etc/cron.hourly)"),
		},
		expect: "Jul 17 16:01:01.416351 /USR/SBIN/CROND[8278]: (root) CMD (run-parts /etc/cron.hourly)\n",
	}, {
		name: "multiline",
		entry: Entry{
			FIELD_REALTIME_TIMESTAMP:        []byte("1342540861421465"),
			FIELD_SOURCE_REALTIME_TIMESTAMP: []byte("1342540861416351"),
			FIELD_SYSLOG_IDENTIFIER:         []byte("/USR/SBIN/CROND"),
			FIELD_SYSLOG_PID:                []byte("8278"),
			FIELD_PID:                       []byte("8278"),
			FIELD_MESSAGE:                   []byte("first\nsecond"),
		},
		expect: "Jul 17 16:01:01.416351 /USR/SBIN/CROND[8278]: first\n                                              second\n",
	}, {
		name: "syslog_pid",
		entry: Entry{
			FIELD_REALTIME_TIMESTAMP:        []byte("1342540861421465"),
			FIELD_SOURCE_REALTIME_TIMESTAMP: []byte("1342540861416351"),
			FIELD_SYSLOG_IDENTIFIER:         []byte("/USR/SBIN/CROND"),
			FIELD_SYSLOG_PID:                []byte("8278"),
			FIELD_MESSAGE:                   []byte("(root) CMD (run-parts /etc/cron.hourly)"),
		},
		expect: "Jul 17 16:01:01.416351 /USR/SBIN/CROND[8278]: (root) CMD (run-parts /etc/cron.hourly)\n",
	}, {
		name: "no_pid",
		entry: Entry{
			FIELD_REALTIME_TIMESTAMP:        []byte("1342540861421465"),
			FIELD_SOURCE_REALTIME_TIMESTAMP: []byte("1342540861416351"),
			FIELD_SYSLOG_IDENTIFIER:         []byte("/USR/SBIN/CROND"),
			FIELD_MESSAGE:                   []byte("(root) CMD (run-parts /etc/cron.hourly)"),
		},
		expect: "Jul 17 16:01:01.416351 /USR/SBIN/CROND: (root) CMD (run-parts /etc/cron.hourly)\n",
	}, {
		name: "no_ident",
		entry: Entry{
			FIELD_REALTIME_TIMESTAMP:        []byte("1342540861421465"),
			FIELD_SOURCE_REALTIME_TIMESTAMP: []byte("1342540861416351"),
			FIELD_SYSLOG_PID:                []byte("8278"),
			FIELD_PID:                       []byte("8278"),
			FIELD_MESSAGE:                   []byte("(root) CMD (run-parts /etc/cron.hourly)"),
		},
		expect: "Jul 17 16:01:01.416351 unknown[8278]: (root) CMD (run-parts /etc/cron.hourly)\n",
	}, {
		name: "no_ident_pid",
		entry: Entry{
			FIELD_REALTIME_TIMESTAMP:        []byte("1342540861421465"),
			FIELD_SOURCE_REALTIME_TIMESTAMP: []byte("1342540861416351"),
			FIELD_MESSAGE:                   []byte("(root) CMD (run-parts /etc/cron.hourly)"),
		},
		expect: "Jul 17 16:01:01.416351 unknown: (root) CMD (run-parts /etc/cron.hourly)\n",
	}, {
		name: "no_source",
		entry: Entry{
			FIELD_REALTIME_TIMESTAMP: []byte("1342540861421465"),
			FIELD_SYSLOG_IDENTIFIER:  []byte("/USR/SBIN/CROND"),
			FIELD_SYSLOG_PID:         []byte("8278"),
			FIELD_PID:                []byte("8278"),
			FIELD_MESSAGE:            []byte("(root) CMD (run-parts /etc/cron.hourly)"),
		},
		expect: "Jul 17 16:01:01.421465 /USR/SBIN/CROND[8278]: (root) CMD (run-parts /etc/cron.hourly)\n",
	}, {
		name: "no_time",
		entry: Entry{
			FIELD_SYSLOG_IDENTIFIER: []byte("/USR/SBIN/CROND"),
			FIELD_SYSLOG_PID:        []byte("8278"),
			FIELD_PID:               []byte("8278"),
			FIELD_MESSAGE:           []byte("(root) CMD (run-parts /etc/cron.hourly)"),
		},
		expect: "",
	}, {
		name: "zero_time",
		entry: Entry{
			FIELD_REALTIME_TIMESTAMP: []byte("0"),
			FIELD_SYSLOG_IDENTIFIER:  []byte("/USR/SBIN/CROND"),
			FIELD_SYSLOG_PID:         []byte("8278"),
			FIELD_PID:                []byte("8278"),
			FIELD_MESSAGE:            []byte("(root) CMD (run-parts /etc/cron.hourly)"),
		},
		expect: "",
	}, {
		name: "nearly_zero_time",
		entry: Entry{
			FIELD_REALTIME_TIMESTAMP: []byte("2"),
			FIELD_SYSLOG_IDENTIFIER:  []byte("/USR/SBIN/CROND"),
			FIELD_SYSLOG_PID:         []byte("8278"),
			FIELD_PID:                []byte("8278"),
			FIELD_MESSAGE:            []byte("(root) CMD (run-parts /etc/cron.hourly)"),
		},
		expect: "",
	}, {
		name: "no_message",
		entry: Entry{
			FIELD_REALTIME_TIMESTAMP:        []byte("1342540861421465"),
			FIELD_SOURCE_REALTIME_TIMESTAMP: []byte("1342540861416351"),
			FIELD_SYSLOG_IDENTIFIER:         []byte("/USR/SBIN/CROND"),
			FIELD_SYSLOG_PID:                []byte("8278"),
			FIELD_PID:                       []byte("8278"),
		},
		expect: "",
	}} {
		t.Run(testcase.name, func(t *testing.T) {
			var buf bytes.Buffer
			w := ShortWriter(&buf)
			w.SetTimezone(time.UTC) // Needed for consistent test results.
			if err := w.WriteEntry(testcase.entry); err != nil {
				t.Error(err)
			}
			if buf.String() != testcase.expect {
				t.Errorf("%q != %q", buf.String(), testcase.expect)
			}
		})
	}
}

func TestFormatShortFromExport(t *testing.T) {
	var buf bytes.Buffer
	er := NewExportReader(strings.NewReader(exportText + exportBinary))
	sw := ShortWriter(&buf)
	sw.SetTimezone(time.UTC)
	for {
		entry, err := er.ReadEntry()
		if err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if err := sw.WriteEntry(entry); err != nil {
			t.Error(err)
		}
	}
	const expect = `Jul 17 16:01:01.413961 gdm-password][587]: AccountsService-DEBUG(+): ActUserManager: ignoring unspecified session '8' since it's not graphical: Success
Jul 17 16:01:01.416351 /USR/SBIN/CROND[8278]: (root) CMD (run-parts /etc/cron.hourly)
-- Reboot --
Feb 14 20:15:16.372858 python3[16853]: foo
                                       bar
`
	if d := diff.Diff(buf.String(), expect); d != "" {
		t.Errorf("unexpected output:\n%s", d)
	}
}
