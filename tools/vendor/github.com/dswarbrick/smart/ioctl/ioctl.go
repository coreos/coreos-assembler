// Copyright 2017-18 Daniel Swarbrick. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Implementation of Linux kernel ioctl macros (<uapi/asm-generic/ioctl.h>).
// See https://www.kernel.org/doc/Documentation/ioctl/ioctl-number.txt

package ioctl

import (
	"golang.org/x/sys/unix"
)

const (
	directionNone  = 0
	directionWrite = 1
	directionRead  = 2

	numberBits    = 8
	typeBits      = 8
	sizeBits      = 14
	directionBits = 2

	numberMask    = (1 << numberBits) - 1
	typeMask      = (1 << typeBits) - 1
	sizeMask      = (1 << sizeBits) - 1
	directionMask = (1 << directionBits) - 1

	numberShift    = 0
	typeShift      = numberShift + numberBits
	sizeShift      = typeShift + typeBits
	directionShift = sizeShift + sizeBits
)

// _ioc calculates the ioctl command for the specified direction, type, number and size
func _ioc(dir, t, nr, size uintptr) uintptr {
	return (dir << directionShift) | (t << typeShift) | (nr << numberShift) | (size << sizeShift)
}

// Ior calculates the ioctl command for a read-ioctl of the specified type, number and size
func Ior(t, nr, size uintptr) uintptr {
	return _ioc(directionRead, t, nr, size)
}

// Iow calculates the ioctl command for a write-ioctl of the specified type, number and size
func Iow(t, nr, size uintptr) uintptr {
	return _ioc(directionWrite, t, nr, size)
}

// Iowr calculates the ioctl command for a read/write-ioctl of the specified type, number and size
func Iowr(t, nr, size uintptr) uintptr {
	return _ioc(directionWrite|directionRead, t, nr, size)
}

// ioctl executes an ioctl command on the specified file descriptor
func Ioctl(fd, cmd, ptr uintptr) error {
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, cmd, ptr)
	if errno != 0 {
		return errno
	}
	return nil
}
