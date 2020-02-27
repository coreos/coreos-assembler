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
	"io"
	"strings"
	"testing"
)

const (
	exportText = `__CURSOR=s=739ad463348b4ceca5a9e69c95a3c93f;i=4ece7;b=6c7c6013a26343b29e964691ff25d04c;m=4fc72436e;t=4c508a72423d9;x=d3e5610681098c10;p=system.journal
__REALTIME_TIMESTAMP=1342540861416409
__MONOTONIC_TIMESTAMP=21415215982
_BOOT_ID=6c7c6013a26343b29e964691ff25d04c
_TRANSPORT=syslog
PRIORITY=4
SYSLOG_FACILITY=3
SYSLOG_IDENTIFIER=gdm-password]
SYSLOG_PID=587
MESSAGE=AccountsService-DEBUG(+): ActUserManager: ignoring unspecified session '8' since it's not graphical: Success
_PID=587
_UID=0
_GID=500
_COMM=gdm-session-wor
_EXE=/usr/libexec/gdm-session-worker
_CMDLINE=gdm-session-worker [pam/gdm-password]
_AUDIT_SESSION=2
_AUDIT_LOGINUID=500
_SYSTEMD_CGROUP=/user/lennart/2
_SYSTEMD_SESSION=2
_SELINUX_CONTEXT=system_u:system_r:xdm_t:s0-s0:c0.c1023
_SOURCE_REALTIME_TIMESTAMP=1342540861413961
_MACHINE_ID=a91663387a90b89f185d4e860000001a
_HOSTNAME=epsilon

__CURSOR=s=739ad463348b4ceca5a9e69c95a3c93f;i=4ece8;b=6c7c6013a26343b29e964691ff25d04c;m=4fc72572f;t=4c508a7243799;x=68597058a89b7246;p=system.journal
__REALTIME_TIMESTAMP=1342540861421465
__MONOTONIC_TIMESTAMP=21415221039
_BOOT_ID=6c7c6013a26343b29e964691ff25d04c
_TRANSPORT=syslog
PRIORITY=6
SYSLOG_FACILITY=9
SYSLOG_IDENTIFIER=/USR/SBIN/CROND
SYSLOG_PID=8278
MESSAGE=(root) CMD (run-parts /etc/cron.hourly)
_PID=8278
_UID=0
_GID=0
_COMM=run-parts
_EXE=/usr/bin/bash
_CMDLINE=/bin/bash /bin/run-parts /etc/cron.hourly
_AUDIT_SESSION=8
_AUDIT_LOGINUID=0
_SYSTEMD_CGROUP=/user/root/8
_SYSTEMD_SESSION=8
_SELINUX_CONTEXT=system_u:system_r:crond_t:s0-s0:c0.c1023
_SOURCE_REALTIME_TIMESTAMP=1342540861416351
_MACHINE_ID=a91663387a90b89f185d4e860000001a
_HOSTNAME=epsilon

`

	exportBinary = `
__CURSOR=s=bcce4fb8ffcb40e9a6e05eee8b7831bf;i=5ef603;b=ec25d6795f0645619ddac9afdef453ee;m=545242e7049;t=50f1202
__REALTIME_TIMESTAMP=1423944916375353
__MONOTONIC_TIMESTAMP=5794517905481
_BOOT_ID=ec25d6795f0645619ddac9afdef453ee
_TRANSPORT=journal
_UID=1001
_GID=1001
_CAP_EFFECTIVE=0
_SYSTEMD_OWNER_UID=1001
_SYSTEMD_SLICE=user-1001.slice
_MACHINE_ID=5833158886a8445e801d437313d25eff
_HOSTNAME=bupkis
_AUDIT_LOGINUID=1001
_SELINUX_CONTEXT=unconfined_u:unconfined_r:unconfined_t:s0-s0:c0.c1023
CODE_LINE=1
CODE_FUNC=<module>
SYSLOG_IDENTIFIER=python3
_COMM=python3
_EXE=/usr/bin/python3.4
_AUDIT_SESSION=35898
_SYSTEMD_CGROUP=/user.slice/user-1001.slice/session-35898.scope
_SYSTEMD_SESSION=35898
_SYSTEMD_UNIT=session-35898.scope
MESSAGE
` + "\x07\x00\x00\x00\x00\x00\x00\x00foo\nbar" + `
CODE_FILE=<string>
_PID=16853
_CMDLINE=python3 -c from systemd import journal; journal.send("foo\nbar")
_SOURCE_REALTIME_TIMESTAMP=1423944916372858

`
)

func TestExportReaderText(t *testing.T) {
	er := NewExportReader(strings.NewReader(exportText))
	if entry, err := er.ReadEntry(); err != nil {
		t.Errorf("first read failed: %v", err)
	} else {
		const expect = "AccountsService-DEBUG(+): ActUserManager: ignoring unspecified session '8' since it's not graphical: Success"
		msg := string(entry[FIELD_MESSAGE])
		if msg != expect {
			t.Errorf("%q != %q", msg, expect)
		}
	}
	if entry, err := er.ReadEntry(); err != nil {
		t.Errorf("second read failed: %v", err)
	} else {
		const expect = "(root) CMD (run-parts /etc/cron.hourly)"
		msg := string(entry[FIELD_MESSAGE])
		if msg != expect {
			t.Errorf("%q != %q", msg, expect)
		}
	}
	if _, err := er.ReadEntry(); err != io.EOF {
		t.Errorf("final read didn't return EOF: %v", err)
	}
}

func TestExportReaderLeadingNewline(t *testing.T) {
	er := NewExportReader(strings.NewReader("\n" + exportText))
	if entry, err := er.ReadEntry(); err != nil {
		t.Errorf("first read failed: %v", err)
	} else {
		const expect = "AccountsService-DEBUG(+): ActUserManager: ignoring unspecified session '8' since it's not graphical: Success"
		msg := string(entry[FIELD_MESSAGE])
		if msg != expect {
			t.Errorf("%q != %q", msg, expect)
		}
	}
	if entry, err := er.ReadEntry(); err != nil {
		t.Errorf("second read failed: %v", err)
	} else {
		const expect = "(root) CMD (run-parts /etc/cron.hourly)"
		msg := string(entry[FIELD_MESSAGE])
		if msg != expect {
			t.Errorf("%q != %q", msg, expect)
		}
	}
	if _, err := er.ReadEntry(); err != io.EOF {
		t.Errorf("final read didn't return EOF: %v", err)
	}
}

func TestExportReaderBinary(t *testing.T) {
	er := NewExportReader(strings.NewReader(exportBinary))
	if entry, err := er.ReadEntry(); err != nil {
		t.Errorf("first read failed: %v", err)
	} else {
		const expect = "foo\nbar"
		msg := string(entry[FIELD_MESSAGE])
		if msg != expect {
			t.Errorf("%q != %q", msg, expect)
		}
	}
	if _, err := er.ReadEntry(); err != io.EOF {
		t.Errorf("final read didn't return EOF: %v", err)
	}
}

func BenchmarkExportReader(b *testing.B) {
	testData := strings.Repeat(exportText+exportBinary, 10)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		er := NewExportReader(strings.NewReader(testData))
		for {
			if _, err := er.ReadEntry(); err == io.EOF {
				break
			} else if err != nil {
				b.Error(err)
			}
		}
	}
}
