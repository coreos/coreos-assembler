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

// NTP v4 (RFC 5905)
// http://tools.ietf.org/html/rfc5905
package ntp

//go:generate stringer -type=LeapIndicator,Mode,VersionNumber -output=protocol_string.go protocol.go

import (
	"encoding/binary"
	"fmt"
	"time"
)

// JAN_1970 is the Unix Epoch in NTP Seconds (1970 - 1900)
const JAN_1970 = 2208988800

// short-hand to make the marshal functions less tedious
var be = binary.BigEndian

// NTP Short Format
//
//	 0                   1                   2                   3
//	 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|          Seconds              |           Fraction            |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
type Short struct {
	Seconds  uint16
	Fraction uint16
}

func (s *Short) marshalBinary(b []byte) {
	be.PutUint16(b[0:], s.Seconds)
	be.PutUint16(b[2:], s.Fraction)
}

func (s *Short) unmarshalBinary(b []byte) {
	s.Seconds = be.Uint16(b[0:])
	s.Fraction = be.Uint16(b[2:])
}

// NTP Timestamp Format
//
//	 0                   1                   2                   3
//	 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                            Seconds                            |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                            Fraction                           |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
type Timestamp struct {
	Seconds  uint32
	Fraction uint32
}

// Now gets the current NTP time in the 64-bit Timestamp format.
func Now() Timestamp {
	return NewTimestamp(time.Now())
}

// NewTimestamp converts from Go's Time to NTP's 64-bit Timestamp format.
func NewTimestamp(t time.Time) Timestamp {
	secs := t.Unix() + JAN_1970
	// Convert from range [0,999999999] to [0,UINT32_MAX]
	frac := (uint64(t.Nanosecond()) << 32) / 1000000000
	return Timestamp{Seconds: uint32(secs), Fraction: uint32(frac)}
}

// Precision represents the accuracy of times reported by Now() for use
// in Header.Precision. The return value is log2 seconds.
func Precision() int8 {
	// The RFC claims this should be determined by the clock resolution
	// or the execution time of reading the clock, whichever is greater.
	// This code is not a stickler for accuracy so just assume the worst
	// and use ~1μs which huge compared to execution time.
	return -20 // log2 1e-6 = -19.93
}

func (t *Timestamp) marshalBinary(b []byte) {
	be.PutUint32(b[0:], t.Seconds)
	be.PutUint32(b[4:], t.Fraction)
}

func (t *Timestamp) unmarshalBinary(b []byte) {
	t.Seconds = be.Uint32(b[0:])
	t.Fraction = be.Uint32(b[4:])
}

// LeapIndicator (LI): 2-bit integer warning of an impending leap second
// to be inserted or deleted in the last minute of the current month.
type LeapIndicator byte

const (
	LEAP_NONE   LeapIndicator = iota
	LEAP_ADD                  // last minute of the day has 61 seconds
	LEAP_SUB                  // last minute of the day has 59 seconds
	LEAP_NOSYNC               // unknown (clock unsynchronized)
)

// VersionNumber (VN): 3-bit integer representing the NTP version
// number, currently 4.
type VersionNumber byte

const NTPv4 VersionNumber = 4

// Mode: 3-bit integer representing the association mode.
type Mode byte

const (
	MODE_RESERVED Mode = iota
	MODE_SYMMETRIC_ACTIVE
	MODE_SYMMETRIC_PASSIVE
	MODE_CLIENT
	MODE_SERVER
	MODE_BROADCAST
	MODE_CONTROL // NTP control message
	MODE_PRIVATE // reserved for private use
)

// NTP Packet Header Format
//
//	 0                   1                   2                   3
//	 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|LI | VN  |Mode |    Stratum    |     Poll      |   Precision   |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                         Root Delay                            |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                         Root Dispersion                       |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                          Reference ID                         |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                                                               |
//	+                     Reference Timestamp (64)                  +
//	|                                                               |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                                                               |
//	+                      Origin Timestamp (64)                    +
//	|                                                               |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                                                               |
//	+                      Receive Timestamp (64)                   +
//	|                                                               |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                                                               |
//	+                      Transmit Timestamp (64)                  +
//	|                                                               |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                                                               |
//	.                                                               .
//	.                    Extension Field 1 (variable)               .
//	.                                                               .
//	|                                                               |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                                                               |
//	.                                                               .
//	.                    Extension Field 2 (variable)               .
//	.                                                               .
//	|                                                               |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                          Key Identifier                       |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                                                               |
//	|                            dgst (128)                         |
//	|                                                               |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
type Header struct {
	LeapIndicator      LeapIndicator // 2 bits
	VersionNumber      VersionNumber // 3 bits
	Mode               Mode          // 3 bits
	Stratum            uint8
	Poll               int8
	Precision          int8
	RootDelay          Short
	RootDispersion     Short
	ReferenceId        [4]byte
	ReferenceTimestamp Timestamp
	OriginTimestamp    Timestamp
	ReceiveTimestamp   Timestamp
	TransmitTimestamp  Timestamp
	// Extension fields and digest not included
}

// magic values for packing a packet
const (
	liMax      = 3
	liOffset   = 6
	vnMax      = 7
	vnOffset   = 3
	modeMax    = 7
	modeOffset = 0
	headerSize = 48 // bytes
)

func (h *Header) MarshalBinary() ([]byte, error) {
	if h.LeapIndicator > liMax ||
		h.VersionNumber > vnMax ||
		h.Mode > modeMax {
		return nil, fmt.Errorf("Invalid NTP Header %v", h)
	}

	data := make([]byte, headerSize)
	data[0] = ((byte(h.LeapIndicator) << liOffset) |
		(byte(h.VersionNumber) << vnOffset) |
		(byte(h.Mode) << modeOffset))
	data[1] = byte(h.Stratum)
	data[2] = byte(h.Poll)
	data[3] = byte(h.Precision)
	h.RootDelay.marshalBinary(data[4:])
	h.RootDispersion.marshalBinary(data[8:])
	copy(data[12:], h.ReferenceId[:])
	h.ReferenceTimestamp.marshalBinary(data[16:])
	h.OriginTimestamp.marshalBinary(data[24:])
	h.ReceiveTimestamp.marshalBinary(data[32:])
	h.TransmitTimestamp.marshalBinary(data[40:])

	return data, nil
}

func (h *Header) UnmarshalBinary(data []byte) error {
	if len(data) < headerSize {
		return fmt.Errorf("NTP packet too small! %d < %d", len(data), headerSize)
	}

	h.LeapIndicator = LeapIndicator((data[0] >> liOffset) & liMax)
	h.VersionNumber = VersionNumber((data[0] >> vnOffset) & vnMax)
	h.Mode = Mode((data[0] >> modeOffset) & modeMax)
	h.Stratum = uint8(data[1])
	h.Poll = int8(data[2])
	h.Precision = int8(data[3])
	h.RootDelay.unmarshalBinary(data[4:])
	h.RootDispersion.unmarshalBinary(data[8:])
	copy(h.ReferenceId[:], data[12:])
	h.ReferenceTimestamp.unmarshalBinary(data[16:])
	h.OriginTimestamp.unmarshalBinary(data[24:])
	h.ReceiveTimestamp.unmarshalBinary(data[32:])
	h.TransmitTimestamp.unmarshalBinary(data[40:])

	return nil
}
