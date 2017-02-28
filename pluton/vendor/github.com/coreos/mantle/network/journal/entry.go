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
	"strconv"
	"time"
)

// Journal entry field strings which correspond to:
// http://www.freedesktop.org/software/systemd/man/systemd.journal-fields.html
const (
	// User Journal Fields
	FIELD_MESSAGE           = "MESSAGE"
	FIELD_MESSAGE_ID        = "MESSAGE_ID"
	FIELD_PRIORITY          = "PRIORITY"
	FIELD_CODE_FILE         = "CODE_FILE"
	FIELD_CODE_LINE         = "CODE_LINE"
	FIELD_CODE_FUNC         = "CODE_FUNC"
	FIELD_ERRNO             = "ERRNO"
	FIELD_SYSLOG_FACILITY   = "SYSLOG_FACILITY"
	FIELD_SYSLOG_IDENTIFIER = "SYSLOG_IDENTIFIER"
	FIELD_SYSLOG_PID        = "SYSLOG_PID"

	// Trusted Journal Fields
	FIELD_PID                       = "_PID"
	FIELD_UID                       = "_UID"
	FIELD_GID                       = "_GID"
	FIELD_COMM                      = "_COMM"
	FIELD_EXE                       = "_EXE"
	FIELD_CMDLINE                   = "_CMDLINE"
	FIELD_CAP_EFFECTIVE             = "_CAP_EFFECTIVE"
	FIELD_AUDIT_SESSION             = "_AUDIT_SESSION"
	FIELD_AUDIT_LOGINUID            = "_AUDIT_LOGINUID"
	FIELD_SYSTEMD_CGROUP            = "_SYSTEMD_CGROUP"
	FIELD_SYSTEMD_SESSION           = "_SYSTEMD_SESSION"
	FIELD_SYSTEMD_UNIT              = "_SYSTEMD_UNIT"
	FIELD_SYSTEMD_USER_UNIT         = "_SYSTEMD_USER_UNIT"
	FIELD_SYSTEMD_OWNER_UID         = "_SYSTEMD_OWNER_UID"
	FIELD_SYSTEMD_SLICE             = "_SYSTEMD_SLICE"
	FIELD_SELINUX_CONTEXT           = "_SELINUX_CONTEXT"
	FIELD_SOURCE_REALTIME_TIMESTAMP = "_SOURCE_REALTIME_TIMESTAMP"
	FIELD_BOOT_ID                   = "_BOOT_ID"
	FIELD_MACHINE_ID                = "_MACHINE_ID"
	FIELD_HOSTNAME                  = "_HOSTNAME"
	FIELD_TRANSPORT                 = "_TRANSPORT"

	// Kernel Journal Fields
	FIELD_KERNEL_DEVICE    = "_KERNEL_DEVICE"
	FIELD_KERNEL_SUBSYSTEM = "_KERNEL_SUBSYSTEM"
	FIELD_UDEV_SYSNAME     = "_UDEV_SYSNAME"
	FIELD_UDEV_DEVNODE     = "_UDEV_DEVNODE"
	FIELD_UDEV_DEVLINK     = "_UDEV_DEVLINK"

	// Fields to log on behalf of a different program
	FIELD_COREDUMP_UNIT            = "COREDUMP_UNIT"
	FIELD_COREDUMP_USER_UNIT       = "COREDUMP_USER_UNIT"
	FIELD_OBJECT_PID               = "OBJECT_PID"
	FIELD_OBJECT_UID               = "OBJECT_UID"
	FIELD_OBJECT_GID               = "OBJECT_GID"
	FIELD_OBJECT_COMM              = "OBJECT_COMM"
	FIELD_OBJECT_EXE               = "OBJECT_EXE"
	FIELD_OBJECT_CMDLINE           = "OBJECT_CMDLINE"
	FIELD_OBJECT_AUDIT_SESSION     = "OBJECT_AUDIT_SESSION"
	FIELD_OBJECT_AUDIT_LOGINUID    = "OBJECT_AUDIT_LOGINUID"
	FIELD_OBJECT_SYSTEMD_CGROUP    = "OBJECT_SYSTEMD_CGROUP"
	FIELD_OBJECT_SYSTEMD_SESSION   = "OBJECT_SYSTEMD_SESSION"
	FIELD_OBJECT_SYSTEMD_UNIT      = "OBJECT_SYSTEMD_UNIT"
	FIELD_OBJECT_SYSTEMD_USER_UNIT = "OBJECT_SYSTEMD_USER_UNIT"
	FIELD_OBJECT_SYSTEMD_OWNER_UID = "OBJECT_SYSTEMD_OWNER_UID"

	// Address Fields
	FIELD_CURSOR              = "__CURSOR"
	FIELD_REALTIME_TIMESTAMP  = "__REALTIME_TIMESTAMP"
	FIELD_MONOTONIC_TIMESTAMP = "__MONOTONIC_TIMESTAMP"
)

// Entry is a map of fields representing a single journal entry.
type Entry map[string][]byte

// Realtime parses the SOURCE_REALTIME_TIMESTAMP or REALTIME_TIMESTAMP
// field, preferring the former when available.
func (e Entry) Realtime() time.Time {
	timestamp, ok := e[FIELD_SOURCE_REALTIME_TIMESTAMP]
	if !ok {
		timestamp, ok = e[FIELD_REALTIME_TIMESTAMP]
		if !ok {
			return time.Time{}
		}
	}

	usec, err := strconv.ParseUint(string(timestamp), 10, 64)
	if err != nil || usec < 1e6 {
		return time.Time{}
	}
	sec := usec / 1e6
	nsec := (usec - (sec * 1e6)) * 1000
	return time.Unix(int64(sec), int64(nsec))
}
