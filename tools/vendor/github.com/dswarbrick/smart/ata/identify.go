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

// ATA IDENTIFY DEVICE response parsing

package ata

import (
	"fmt"

	"github.com/dswarbrick/smart/utils"
)

// Table 10 of X3T13/2008D (ATA-3) Revision 7b, January 27, 1997
// Table 28 of T13/1410D (ATA/ATAPI-6) Revision 3b, February 26, 2002
// Table 31 of T13/1699-D (ATA8-ACS) Revision 6a, September 6, 2008
// Table 52 of T13/BSR INCITS 529 (ACS-4) Revision 14, October 14, 2016
var ataMinorVersions = map[uint16]string{
	0x0001: "ATA-1 X3T9.2/781D prior to revision 4",      // obsolete
	0x0002: "ATA-1 published, ANSI X3.221-1994",          // obsolete
	0x0003: "ATA-1 X3T9.2/781D revision 4",               // obsolete
	0x0004: "ATA-2 published, ANSI X3.279-1996",          // obsolete
	0x0005: "ATA-2 X3T10/948D prior to revision 2k",      // obsolete
	0x0006: "ATA-3 X3T10/2008D revision 1",               // obsolete
	0x0007: "ATA-2 X3T10/948D revision 2k",               // obsolete
	0x0008: "ATA-3 X3T10/2008D revision 0",               // obsolete
	0x0009: "ATA-2 X3T10/948D revision 3",                // obsolete
	0x000a: "ATA-3 published, ANSI X3.298-1997",          // obsolete
	0x000b: "ATA-3 X3T10/2008D revision 6",               // obsolete
	0x000c: "ATA-3 X3T13/2008D revision 7 and 7a",        // obsolete
	0x000d: "ATA/ATAPI-4 X3T13/1153D revision 6",         // obsolete
	0x000e: "ATA/ATAPI-4 T13/1153D revision 13",          // obsolete
	0x000f: "ATA/ATAPI-4 X3T13/1153D revision 7",         // obsolete
	0x0010: "ATA/ATAPI-4 T13/1153D revision 18",          // obsolete
	0x0011: "ATA/ATAPI-4 T13/1153D revision 15",          // obsolete
	0x0012: "ATA/ATAPI-4 published, ANSI NCITS 317-1998", // obsolete
	0x0013: "ATA/ATAPI-5 T13/1321D revision 3",
	0x0014: "ATA/ATAPI-4 T13/1153D revision 14", // obsolete
	0x0015: "ATA/ATAPI-5 T13/1321D revision 1",
	0x0016: "ATA/ATAPI-5 published, ANSI NCITS 340-2000",
	0x0017: "ATA/ATAPI-4 T13/1153D revision 17", // obsolete
	0x0018: "ATA/ATAPI-6 T13/1410D revision 0",
	0x0019: "ATA/ATAPI-6 T13/1410D revision 3a",
	0x001a: "ATA/ATAPI-7 T13/1532D revision 1",
	0x001b: "ATA/ATAPI-6 T13/1410D revision 2",
	0x001c: "ATA/ATAPI-6 T13/1410D revision 1",
	0x001d: "ATA/ATAPI-7 published, ANSI INCITS 397-2005",
	0x001e: "ATA/ATAPI-7 T13/1532D revision 0",
	0x001f: "ACS-3 T13/2161-D revision 3b",
	0x0021: "ATA/ATAPI-7 T13/1532D revision 4a",
	0x0022: "ATA/ATAPI-6 published, ANSI INCITS 361-2002",
	0x0027: "ATA8-ACS T13/1699-D revision 3c",
	0x0028: "ATA8-ACS T13/1699-D revision 6",
	0x0029: "ATA8-ACS T13/1699-D revision 4",
	0x0031: "ACS-2 T13/2015-D revision 2",
	0x0033: "ATA8-ACS T13/1699-D revision 3e",
	0x0039: "ATA8-ACS T13/1699-D revision 4c",
	0x0042: "ATA8-ACS T13/1699-D revision 3f",
	0x0052: "ATA8-ACS T13/1699-D revision 3b",
	0x005e: "ACS-4 T13/BSR INCITS 529 revision 5",
	0x006d: "ACS-3 T13/2161-D revision 5",
	0x0082: "ACS-2 published, ANSI INCITS 482-2012",
	0x0107: "ATA8-ACS T13/1699-D revision 2d",
	0x010a: "ACS-3 published, ANSI INCITS 522-2014",
	0x0110: "ACS-2 T13/2015-D revision 3",
	0x011b: "ACS-3 T13/2161-D revision 4",
}

// ATA IDENTIFY DEVICE struct. ATA8-ACS defines this as a page of 16-bit words. Some fields span
// multiple words (e.g., model number), but must (?) be byteswapped. Some fields use less than a
// single word, and are bitmasked together with other fields. Since many of the fields are now
// retired / obsolete, we only define the fields that are currently used by this package.
type IdentifyDeviceData struct {
	GeneralConfig       uint16      // Word 0, general configuration. If bit 15 is zero, device is ATA.
	_                   [9]uint16   // ...
	SerialNumberRaw     [20]byte    // Word 10..19, device serial number, padded with spaces (20h).
	_                   [3]uint16   // ...
	FirmwareRevisionRaw [8]byte     // Word 23..26, device firmware revision, padded with spaces (20h).
	ModelNumberRaw      [40]byte    // Word 27..46, device model number, padded with spaces (20h).
	_                   [28]uint16  // ...
	SATACap             uint16      // Word 76, SATA capabilities.
	SATACapAddl         uint16      // Word 77, SATA additional capabilities.
	_                   [3]uint16   // ...
	MajorVersion        uint16      // Word 80, major version number.
	MinorVersion        uint16      // Word 81, minor version number.
	_                   [3]uint16   // ...
	Word85              uint16      // Word 85, supported commands and feature sets.
	_                   uint16      // ...
	Word87              uint16      // Word 87, supported commands and feature sets.
	_                   [20]uint16  // ...
	WWNRaw              [4]uint16   // Word 108..111, WWN (World Wide Name).
	_                   [105]uint16 // ...
	RotationRate        uint16      // Word 217, nominal media rotation rate.
	_                   [4]uint16   // ...
	TransportMajor      uint16      // Word 222, transport major version number.
	_                   [33]uint16  // ...
} // 512 bytes

// ATAMajorVersion returns the ATA major version from an ATA IDENTIFY command.
func (d *IdentifyDeviceData) ATAMajorVersion() (s string) {
	if (d.MajorVersion == 0) || (d.MajorVersion == 0xffff) {
		s = "device does not report ATA major version"
		return
	}

	switch utils.Log2b(uint(d.MajorVersion)) {
	case 1:
		s = "ATA-1"
	case 2:
		s = "ATA-2"
	case 3:
		s = "ATA-3"
	case 4:
		s = "ATA/ATAPI-4"
	case 5:
		s = "ATA/ATAPI-5"
	case 6:
		s = "ATA/ATAPI-6"
	case 7:
		s = "ATA/ATAPI-7"
	case 8:
		s = "ATA8-ACS"
	case 9:
		s = "ACS-2"
	case 10:
		s = "ACS-3"
	}

	return
}

// ATAMinorVersion returns the ATA minor version from an ATA IDENTIFY command.
func (d *IdentifyDeviceData) ATAMinorVersion() string {
	if (d.MinorVersion == 0) || (d.MinorVersion == 0xffff) {
		return "device does not report ATA minor version"
	}

	// Since the ATA minor version word is not a bitmask, we simply do a map lookup
	if s, ok := ataMinorVersions[d.MinorVersion]; ok {
		return s
	}

	return "unknown"
}

// FirmwareVersion returns the firmware version of a device from an ATA IDENTIFY command.
func (d *IdentifyDeviceData) FirmwareRevision() []byte {
	return d.swapBytes(d.FirmwareRevisionRaw[:])
}

// ModelNumber returns the model number of a device from an ATA IDENTIFY command.
func (d *IdentifyDeviceData) ModelNumber() []byte {
	return d.swapBytes(d.ModelNumberRaw[:])
}

// ModelNumber returns the serial number of a device from an ATA IDENTIFY command.
func (d *IdentifyDeviceData) SerialNumber() []byte {
	return d.swapBytes(d.SerialNumberRaw[:])
}

func (d *IdentifyDeviceData) Transport() (s string) {
	if (d.TransportMajor == 0) || (d.TransportMajor == 0xffff) {
		s = "device does not report transport"
		return
	}

	switch d.TransportMajor >> 12 {
	case 0x0:
		s = "Parallel ATA"
	case 0x1:
		s = "Serial ATA"

		// TODO: Add decoding of current / max SATA speed (word 76, 77)
		switch utils.Log2b(uint(d.TransportMajor & 0x0fff)) {
		case 0:
			s += " ATA8-AST"
		case 1:
			s += " SATA 1.0a"
		case 2:
			s += " SATA II Ext"
		case 3:
			s += " SATA 2.5"
		case 4:
			s += " SATA 2.6"
		case 5:
			s += " SATA 3.0"
		case 6:
			s += " SATA 3.1"
		case 7:
			s += " SATA 3.2"
		default:
			s += fmt.Sprintf(" SATA (%#03x)", d.TransportMajor&0x0fff)
		}
	case 0xe:
		s = fmt.Sprintf("PCIe (%#03x)", d.TransportMajor&0x0fff)
	default:
		s = fmt.Sprintf("Unknown (%#04x)", d.TransportMajor)
	}

	return
}

func (d *IdentifyDeviceData) WWN() string {
	naa := d.WWNRaw[0] >> 12
	oui := (uint32(d.WWNRaw[0]&0x0fff) << 12) | (uint32(d.WWNRaw[1]) >> 4)
	uniqueID := ((uint64(d.WWNRaw[1]) & 0xf) << 32) | (uint64(d.WWNRaw[2]) << 16) | uint64(d.WWNRaw[3])

	return fmt.Sprintf("%x %06x %09x", naa, oui, uniqueID)
}

func (d *IdentifyDeviceData) swapBytes(b []byte) []byte {
	tmp := make([]byte, len(b))

	for i := 0; i < len(b); i += 2 {
		tmp[i], tmp[i+1] = b[i+1], b[i]
	}

	return tmp
}
