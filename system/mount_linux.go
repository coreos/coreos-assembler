// Copyright 2015 CoreOS, Inc.
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

package system

import (
	"fmt"
	"strings"
	"syscall"
)

const (
	// MS_PROPAGATION flags are special operations and cannot be combined
	// with each other or any flags other than MS_REC.
	MS_PROPAGATION = syscall.MS_SHARED | syscall.MS_SLAVE | syscall.MS_UNBINDABLE | syscall.MS_PRIVATE
	// MS_OPERATION flags can be mapped to high level operation names.
	MS_OPERATION = MS_PROPAGATION | syscall.MS_BIND | syscall.MS_MOVE | syscall.MS_REC
)

// map mount flags to higher level "operation" names
var mountOps = map[uintptr]string{
	syscall.MS_BIND:                        "bind",
	syscall.MS_BIND | syscall.MS_REC:       "rbind",
	syscall.MS_MOVE:                        "move",
	syscall.MS_SILENT:                      "silent",
	syscall.MS_UNBINDABLE:                  "unbindable",
	syscall.MS_UNBINDABLE | syscall.MS_REC: "runbindable",
	syscall.MS_PRIVATE:                     "private",
	syscall.MS_PRIVATE | syscall.MS_REC:    "rprivate",
	syscall.MS_SLAVE:                       "slave",
	syscall.MS_SLAVE | syscall.MS_REC:      "rslave",
	syscall.MS_SHARED:                      "shared",
	syscall.MS_SHARED | syscall.MS_REC:     "rshared",
}

// map mount flag strings to the numeric value.
// names match mount(8) except where otherwise noted
var mountFlags = map[string]uintptr{
	"ro":          syscall.MS_RDONLY,
	"nosuid":      syscall.MS_NOSUID,
	"nodev":       syscall.MS_NODEV,
	"noexec":      syscall.MS_NOEXEC,
	"sync":        syscall.MS_SYNCHRONOUS,
	"remount":     syscall.MS_REMOUNT,
	"mand":        syscall.MS_MANDLOCK,
	"dirsync":     syscall.MS_DIRSYNC,
	"noatime":     syscall.MS_NOATIME,
	"nodiratime":  syscall.MS_NODIRATIME,
	"bind":        syscall.MS_BIND,
	"rbind":       syscall.MS_BIND | syscall.MS_REC,
	"x-move":      syscall.MS_MOVE, // --move
	"silent":      syscall.MS_SILENT,
	"unbindable":  syscall.MS_UNBINDABLE,
	"runbindable": syscall.MS_UNBINDABLE | syscall.MS_REC,
	"private":     syscall.MS_PRIVATE,
	"rprivate":    syscall.MS_PRIVATE | syscall.MS_REC,
	"slave":       syscall.MS_SLAVE,
	"rslave":      syscall.MS_SLAVE | syscall.MS_REC,
	"shared":      syscall.MS_SHARED,
	"rshared":     syscall.MS_SHARED | syscall.MS_REC,
	"relatime":    syscall.MS_RELATIME,
	"iversion":    syscall.MS_I_VERSION,
	"strictatime": syscall.MS_STRICTATIME,
}

// MountError records a mount operation failure, similar to os.PathError
type MountError struct {
	Source string
	Target string
	FsType string
	Flags  uintptr
	Extra  string
	Err    error
}

func (e *MountError) Error() string {
	op, ok := mountOps[e.Flags&MS_OPERATION]
	if !ok {
		op = "mount"
	}
	if e.Flags&MS_PROPAGATION != 0 {
		// Source is unused for these operations.
		return fmt.Sprintf("%s on %s failed: %v", op, e.Target, e.Err)
	}
	return fmt.Sprintf("%s %s to %s failed: %v", op, e.Source, e.Target, e.Err)
}

func splitFlags(options string) (uintptr, string) {
	var flags uintptr
	var extra []string
	for _, opt := range strings.Split(options, ",") {
		if flag, ok := mountFlags[opt]; ok {
			flags |= flag
		} else {
			extra = append(extra, opt)
		}
	}
	return flags, strings.Join(extra, ",")
}

func doMount(source, target, fstype string, flags uintptr, extra string) error {
	if err := syscall.Mount(source, target, fstype, flags, extra); err != nil {
		return &MountError{
			Source: source,
			Target: target,
			FsType: fstype,
			Flags:  flags,
			Extra:  extra,
			Err:    err,
		}
	}
	return nil
}

// Mount wraps mount(2) in a similar way to mount(8), accepting both flags
// and filesystem options as a string. Any option not recognized as a flag
// will be passed as a filesystem option. Note that option parsing here is
// simpler than mount(8) and quotes are not considered.
func Mount(source, target, fstype, options string) error {
	// A simple default for virtual filesystems
	if source == "" {
		source = fstype
	}
	flags, extra := splitFlags(options)
	return doMount(source, target, fstype, flags, extra)
}

// Bind creates a bind mount from source to target.
func Bind(source, target string) error {
	return doMount(source, target, "none", syscall.MS_BIND, "")
}

// ReadOnlyBind creates a read-only bind mount. Note that this must be
// performed in two operations so it is possible for a read-write bind
// to be left behind if the second operation fails.
func ReadOnlyBind(source, target string) error {
	var flags uintptr = syscall.MS_BIND
	if err := doMount(source, target, "none", flags, ""); err != nil {
		return err
	}
	flags |= syscall.MS_REMOUNT | syscall.MS_RDONLY
	return doMount(source, target, "none", flags, "")
}

// RecursiveBind bind mounts an entire tree under source to target.
func RecursiveBind(source, target string) error {
	return doMount(source, target, "none", syscall.MS_BIND|syscall.MS_REC, "")
}

// Move moves an entire tree under the source mountpoint to target.
func Move(source, target string) error {
	return doMount(source, target, "none", syscall.MS_MOVE, "")
}

// MountPrivate changes a mount point's propagation type to "private"
func MountPrivate(target string) error {
	return doMount("none", target, "none", syscall.MS_PRIVATE, "")
}

// RecursivePrivate changes an entire tree's propagation type to "private"
func RecursivePrivate(target string) error {
	return doMount("none", target, "none", syscall.MS_PRIVATE|syscall.MS_REC, "")
}

// MountShared changes a mount point's propagation type to "shared"
func MountShared(target string) error {
	return doMount("none", target, "none", syscall.MS_SHARED, "")
}

// RecursiveShared changes an entire tree's propagation type to "shared"
func RecursiveShared(target string) error {
	return doMount("none", target, "none", syscall.MS_SHARED|syscall.MS_REC, "")
}

// MountSlave changes a mount point's propagation type to "slave"
func MountSlave(target string) error {
	return doMount("none", target, "none", syscall.MS_SLAVE, "")
}

// RecursiveSlave changes an entire tree's propagation type to "slave"
func RecursiveSlave(target string) error {
	return doMount("none", target, "none", syscall.MS_SLAVE|syscall.MS_REC, "")
}
