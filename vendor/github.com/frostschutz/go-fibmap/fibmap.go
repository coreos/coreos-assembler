// Copyright (C) 2014 Andreas Klauer <Andreas.Klauer@metamorpher.de>
// License: MIT

// Package fibmap implements FIBMAP/FIEMAP and related Linux ioctl
// for dealing with low level file allocation.
package fibmap

import (
	"os"
	"syscall"
	"unsafe"
)

const (
	FiemapExtentSize = 56 // sizeof(struct fiemap_extent)
	FiemapSize       = 32 // sizeof(struct fiemap)

	// Defined in <linux/fs.h>:
	SEEK_SET      = 0 // seek relative to beginning of file
	SEEK_CUR      = 1 // seek relative to current file position
	SEEK_END      = 2 // seek relative to end of file
	SEEK_DATA     = 3 // seek to the next data
	SEEK_HOLE     = 4 // seek to the next hole
	SEEK_MAX      = SEEK_HOLE
	FIBMAP        = 1 // bmap access
	FIGETBSZ      = 2 // get the block size used for bmap
	FS_IOC_FIEMAP = 3223348747

	// Defined in <linux/fiemap.h>:
	FIEMAP_MAX_OFFSET            = ^uint64(0)
	FIEMAP_FLAG_SYNC             = 0x0001 // sync file data before map
	FIEMAP_FLAG_XATTR            = 0x0002 // map extended attribute tree
	FIEMAP_FLAG_CACHE            = 0x0004 // request caching of the extents
	FIEMAP_FLAGS_COMPAT          = (FIEMAP_FLAG_SYNC | FIEMAP_FLAG_XATTR)
	FIEMAP_EXTENT_LAST           = 0x0001 // Last extent in file.
	FIEMAP_EXTENT_UNKNOWN        = 0x0002 // Data location unknown.
	FIEMAP_EXTENT_DELALLOC       = 0x0004 // Location still pending. Sets EXTENT_UNKNOWN.
	FIEMAP_EXTENT_ENCODED        = 0x0008 // Data can not be read while fs is unmounted
	FIEMAP_EXTENT_DATA_ENCRYPTED = 0x0080 // Data is encrypted by fs. Sets EXTENT_NO_BYPASS.
	FIEMAP_EXTENT_NOT_ALIGNED    = 0x0100 // Extent offsets may not be block aligned.
	FIEMAP_EXTENT_DATA_INLINE    = 0x0200 // Data mixed with metadata. Sets EXTENT_NOT_ALIGNED.
	FIEMAP_EXTENT_DATA_TAIL      = 0x0400 // Multiple files in block. Sets EXTENT_NOT_ALIGNED.
	FIEMAP_EXTENT_UNWRITTEN      = 0x0800 // Space allocated, but no data (i.e. zero).
	FIEMAP_EXTENT_MERGED         = 0x1000 // File does not natively support extents. Result merged for efficiency.
	FIEMAP_EXTENT_SHARED         = 0x2000 // Space shared with other files.

	// Defined in <linux/falloc.h>:
	FALLOC_FL_KEEP_SIZE    = 0x01 // default is extend size
	FALLOC_FL_PUNCH_HOLE   = 0x02 // de-allocates range
	FALLOC_FL_NO_HIDE_STAE = 0x04 // reserved codepoint
)

// based on struct fiemap from <linux/fiemap.h>
type fiemap struct {
	Start          uint64 // logical offset (inclusive) at which to start mapping (in)
	Length         uint64 // logical length of mapping which userspace wants (in)
	Flags          uint32 // FIEMAP_FLAG_* flags for request (in/out)
	Mapped_extents uint32 // number of extents that were mapped (out)
	Extent_count   uint32 // size of fm_extents array (in)
	Reserved       uint32
	// Extents [0]Extent // array of mapped extents (out)
}

// based on struct fiemap_extent from <linux/fiemap.h>
type Extent struct {
	Logical    uint64 // logical offset in bytes for the start of the extent from the beginning of the file
	Physical   uint64 // physical offset in bytes for the start	of the extent from the beginning of the disk
	Length     uint64 // length in bytes for this extent
	Reserved64 [2]uint64
	Flags      uint32 // FIEMAP_EXTENT_* flags for this extent
	Reserved   [3]uint32
}

type FibmapFile struct {
	*os.File
}

// return a new FibmapFile
func NewFibmapFile(f *os.File) FibmapFile {
	return FibmapFile{f}
}

// call FIBMAP ioctl
func (f FibmapFile) Fibmap(block uint) (uint, syscall.Errno) {
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), FIBMAP, uintptr(unsafe.Pointer(&block)))
	return block, err
}

// emulate FIEMAP with FIBMAP
func (f FibmapFile) FibmapExtents() ([]Extent, syscall.Errno) {
	result := make([]Extent, 0)

	bsz, err := f.Figetbsz()
	if err != 0 {
		return nil, err
	}

	stat, _ := f.Stat()
	size := stat.Size()
	if size == 0 {
		return result, syscall.Errno(0)
	}

	blocks := uint((size-1)/int64(bsz)) + 1
	var block, physical, length uint
	var e Extent

	for i := uint(0); i < blocks; i++ {
		block = i
		_, _, err = syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), FIBMAP, uintptr(unsafe.Pointer(&block)))
		if block == 0 && physical == 0 {
			// hole
			continue
		}

		if block == 0 {
			// hole after extent
			e.Length = uint64(length) * uint64(bsz)
			result = append(result, e)
			length = 0
			physical = 0
			continue
		}

		if block == physical {
			// segment
			length++
			physical++
			continue
		}

		if block != physical && physical != 0 {
			//not continuous segment
			e.Length = uint64(length) * uint64(bsz)
			result = append(result, e)
		}
		
		// new extent
		e = Extent{}
		e.Logical = uint64(i) * uint64(bsz)
		e.Physical = uint64(block) * uint64(bsz)

		length = 1
		physical = block + 1
	}

	if length > 0 {
		// final extent
		e.Length = uint64(length) * uint64(bsz)
		result = append(result, e)
	}

	return result, err
}

// call FIEMAP ioctl
func (f FibmapFile) Fiemap(size uint32) ([]Extent, syscall.Errno) {
	extents := make([]Extent, size+1)
	ptr := unsafe.Pointer(uintptr(unsafe.Pointer(&extents[1])) - FiemapSize)

	t := (*fiemap)(ptr)
	t.Start = 0
	t.Length = FIEMAP_MAX_OFFSET
	t.Flags = FIEMAP_FLAG_SYNC
	t.Extent_count = size

	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), FS_IOC_FIEMAP, uintptr(ptr))

	return extents[1 : 1+t.Mapped_extents], err
}

// call FIGETBSZ ioctl
func (f FibmapFile) Figetbsz() (int, syscall.Errno) {
	bsz := int(0)
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), FIGETBSZ, uintptr(unsafe.Pointer(&bsz)))
	return bsz, err
}

// use SEEK_DATA, SEEK_HOLE to find allocated data ranges in a file
func (f FibmapFile) SeekDataHole() []int64 {
	old, _ := f.Seek(0, os.SEEK_CUR)

	var data, hole int64
	var datahole []int64

	for {
		data, _ = f.Seek(hole, SEEK_DATA)

		if data >= hole {
			hole, _ = f.Seek(data, SEEK_HOLE)

			if hole > data {
				datahole = append(datahole, data, hole-data)
				continue
			}
		}
		break
	}

	f.Seek(old, os.SEEK_SET)

	return datahole
}

// allocate using fallocate
func (f FibmapFile) Fallocate(offset int64, length int64) error {
	return syscall.Fallocate(int(f.Fd()), 0, offset, length)
}

// punch hole using fallocate
func (f FibmapFile) PunchHole(offset int64, length int64) error {
	return syscall.Fallocate(int(f.Fd()), FALLOC_FL_KEEP_SIZE|FALLOC_FL_PUNCH_HOLE, offset, length)
}
