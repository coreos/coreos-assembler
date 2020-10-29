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

package ata

import (
	"fmt"
	"io"
	"strconv"

	"github.com/dswarbrick/smart/drivedb"
)

// Individual SMART attribute (12 bytes)
type smartAttr struct {
	Id          uint8
	Flags       uint16
	Value       uint8   // normalised value
	Worst       uint8   // worst value
	VendorBytes [6]byte // vendor-specific (and sometimes device-specific) data
	Reserved    uint8
}

// Page of 30 SMART attributes as per ATA spec
type SmartPage struct {
	Version uint16
	Attrs   [30]smartAttr
}

// SMART log address 00h
type SmartLogDirectory struct {
	Version uint16
	Address [255]struct {
		NumPages byte
		_        byte // Reserved
	}
}

// SMART log address 01h
type SmartSummaryErrorLog struct {
	Version    byte
	LogIndex   byte
	LogData    [5][90]byte // TODO: Expand out to error log structure
	ErrorCount uint16      // Device error count
	_          [57]byte    // Reserved
	Checksum   byte        // Two's complement checksum of first 511 bytes
}

// SMART log address 06h
type SmartSelfTestLog struct {
	Version uint16
	Entry   [21]struct {
		LBA_7          byte   // Content of the LBA field (7:0) when subcommand was issued
		Status         byte   // Self-test execution status
		LifeTimestamp  uint16 // Power-on lifetime of the device in hours when subcommand was completed
		Checkpoint     byte
		LBA            uint32 // LBA of first error (28-bit addressing)
		VendorSpecific [15]byte
	}
	VendorSpecific uint16
	Index          byte
	_              uint16 // Reserved
	Checksum       byte   // Two's complement checksum of first 511 bytes
}

// decodeVendorBytes decodes the six-byte vendor byte array based on the conversion rule passed as
// conv. The conversion may also include the reserved byte, normalised value or worst value byte.
func (sa *smartAttr) decodeVendorBytes(conv string) uint64 {
	var (
		byteOrder string
		r         uint64
	)

	// Default byte orders if not otherwise specified in drivedb
	switch conv {
	case "raw64", "hex64":
		byteOrder = "543210wv"
	case "raw56", "hex56", "raw24/raw32", "msec24hour32":
		byteOrder = "r543210"
	default:
		byteOrder = "543210"
	}

	// Pick bytes from smartAttr in order specified by byteOrder
	for _, i := range byteOrder {
		var b byte

		switch i {
		case '0', '1', '2', '3', '4', '5':
			b = sa.VendorBytes[i-48]
		case 'r':
			b = sa.Reserved
		case 'v':
			b = sa.Value
		case 'w':
			b = sa.Worst
		default:
			b = 0
		}

		r <<= 8
		r |= uint64(b)
	}

	return r
}

func checkTempRange(t int8, ut1, ut2 uint8, lo, hi *int) bool {
	t1, t2 := int8(ut1), int8(ut2)

	if t1 > t2 {
		t1, t2 = t2, t1
	}

	if (-60 <= t1) && (t1 <= t) && (t <= t2) && (t2 <= 120) && !(t1 == -1 && t2 <= 0) {
		*lo, *hi = int(t1), int(t2)
		return true
	}

	return false
}

func checkTempWord(word uint16) int {
	switch {
	case word <= 0x7f:
		return 0x11 // >= 0, signed byte or word
	case word <= 0xff:
		return 0x01 // < 0, signed byte
	case word > 0xff80:
		return 0x10 // < 0, signed word
	default:
		return 0x00
	}
}

func formatRawValue(v uint64, conv string) (s string) {
	var (
		raw  [6]uint8
		word [3]uint16
	)

	// Split into bytes
	for i := 0; i < 6; i++ {
		raw[i] = uint8(v >> uint(i*8))
	}

	// Split into words
	for i := 0; i < 3; i++ {
		word[i] = uint16(v >> uint(i*16))
	}

	switch conv {
	case "raw8":
		s = fmt.Sprintf("%d %d %d %d %d %d",
			raw[5], raw[4], raw[3], raw[2], raw[1], raw[0])
	case "raw16":
		s = fmt.Sprintf("%d %d %d", word[2], word[1], word[0])
	case "raw48", "raw56", "raw64":
		s = fmt.Sprintf("%d", v)
	case "hex48":
		s = fmt.Sprintf("%#012x", v)
	case "hex56":
		s = fmt.Sprintf("%#014x", v)
	case "hex64":
		s = fmt.Sprintf("%#016x", v)
	case "raw16(raw16)":
		s = fmt.Sprintf("%d", word[0])
		if (word[1] != 0) || (word[2] != 0) {
			s += fmt.Sprintf(" (%d %d)", word[2], word[1])
		}
	case "raw16(avg16)":
		s = fmt.Sprintf("%d", word[0])
		if word[1] != 0 {
			s += fmt.Sprintf(" (Average %d)", word[1])
		}
	case "raw24(raw8)":
		s = fmt.Sprintf("%d", v&0x00ffffff)
		if (raw[3] != 0) || (raw[4] != 0) || (raw[5] != 0) {
			s += fmt.Sprintf(" (%d %d %d)", raw[5], raw[4], raw[3])
		}
	case "raw24/raw24":
		s = fmt.Sprintf("%d/%d", v>>24, v&0x00ffffff)
	case "raw24/raw32":
		s = fmt.Sprintf("%d/%d", v>>32, v&0xffffffff)
	case "min2hour":
		// minutes
		minutes := uint64(word[0]) + uint64(word[1])<<16
		s = fmt.Sprintf("%dh+%02dm", minutes/60, minutes%60)
		if word[2] != 0 {
			s += fmt.Sprintf(" (%d)", word[2])
		}
	case "sec2hour":
		// seconds
		hours := v / 3600
		minutes := (v % 3600) / 60
		seconds := v % 60
		s = fmt.Sprintf("%dh+%02dm+%02ds", hours, minutes, seconds)
	case "halfmin2hour":
		// 30-second counter
		hours := v / 120
		minutes := (v % 120) / 2
		s = fmt.Sprintf("%dh+%02dm", hours, minutes)
	case "msec24hour32":
		// hours + milliseconds
		hours := v & 0xffffffff
		milliseconds := v >> 32
		seconds := milliseconds / 1000
		s = fmt.Sprintf("%dh+%02dm+%02d.%03ds",
			hours, seconds/60, seconds%60, milliseconds)
	case "tempminmax":
		var tFormat, lo, hi int

		t := int8(raw[0])
		ctw0 := checkTempWord(word[0])

		if word[2] == 0 {
			if (word[1] == 0) && (ctw0 != 0) {
				// 00 00 00 00 xx TT
				tFormat = 0
			} else if (ctw0 != 0) && checkTempRange(t, raw[2], raw[3], &lo, &hi) {
				// 00 00 HL LH xx TT
				tFormat = 1
			} else if (raw[3] == 0) && checkTempRange(t, raw[1], raw[2], &lo, &hi) {
				// 00 00 00 HL LH TT
				tFormat = 2
			} else {
				tFormat = -1
			}
		} else if ctw0 != 0 {
			if (ctw0&checkTempWord(word[1])&checkTempWord(word[2]) != 0x00) && checkTempRange(t, raw[2], raw[4], &lo, &hi) {
				// xx HL xx LH xx TT
				tFormat = 3
			} else if (word[2] < 0x7fff) && checkTempRange(t, raw[2], raw[3], &lo, &hi) && (hi >= 40) {
				// CC CC HL LH xx TT
				tFormat = 4
			} else {
				tFormat = -2
			}
		} else {
			tFormat = -3
		}

		switch tFormat {
		case 0:
			s = fmt.Sprintf("%d", t)
		case 1, 2, 3:
			s = fmt.Sprintf("%d (Min/Max %d/%d)", t, lo, hi)
		case 4:
			s = fmt.Sprintf("%d (Min/Max %d/%d #%d)", t, lo, hi, word[2])
		default:
			s = fmt.Sprintf("%d (%d %d %d %d %d)",
				raw[0], raw[5], raw[4], raw[3], raw[2], raw[1])
		}
	case "temp10x":
		// ten times temperature in Celsius
		s = fmt.Sprintf("%d.%d", word[0]/10, word[0]%10)
	default:
		s = "?"
	}

	return s
}

func PrintSMARTPage(smart SmartPage, drive drivedb.DriveModel, w io.Writer) {
	fmt.Fprintf(w, "\nSMART structure version: %d\n", smart.Version)
	fmt.Fprintf(w, "ID# ATTRIBUTE_NAME           FLAG     VALUE WORST RESERVED TYPE     UPDATED RAW_VALUE\n")

	for _, attr := range smart.Attrs {
		var (
			rawValue              uint64
			conv                  drivedb.AttrConv
			attrType, attrUpdated string
		)

		if attr.Id == 0 {
			break
		}

		conv, ok := drive.Presets[strconv.Itoa(int(attr.Id))]
		if ok {
			rawValue = attr.decodeVendorBytes(conv.Conv)
		}

		// Pre-fail / advisory bit
		if attr.Flags&0x0001 != 0 {
			attrType = "Pre-fail"
		} else {
			attrType = "Old_age"
		}

		// Online data collection bit
		if attr.Flags&0x0002 != 0 {
			attrUpdated = "Always"
		} else {
			attrUpdated = "Offline"
		}

		fmt.Fprintf(w, "%3d %-24s %#04x   %03d   %03d   %03d      %-8s %-7s %s\n",
			attr.Id, conv.Name, attr.Flags, attr.Value, attr.Worst, attr.Reserved, attrType,
			attrUpdated, formatRawValue(rawValue, conv.Conv))
	}
}
