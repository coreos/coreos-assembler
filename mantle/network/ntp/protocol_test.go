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

package ntp

import (
	"testing"
	"time"

	"github.com/kylelemons/godebug/pretty"
)

type testHeader struct {
	Header
	Raw []byte
}

var testData = []testHeader{
	{Header{
		LeapIndicator:  LEAP_NOSYNC,
		VersionNumber:  NTPv4,
		Mode:           MODE_CLIENT,
		Poll:           3,
		Precision:      -6,
		RootDelay:      Short{Seconds: 1},
		RootDispersion: Short{Seconds: 1},
		TransmitTimestamp: Timestamp{
			Seconds: 0xd90b4fca, Fraction: 0x77ce2d4e},
	}, []byte{
		0xe3, 0x00, 0x03, 0xfa, 0x00, 0x01, 0x00, 0x00,
		0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0xd9, 0x0b, 0x4f, 0xca, 0x77, 0xce, 0x2d, 0x4e,
	}},
	{Header{
		LeapIndicator: LEAP_NONE,
		VersionNumber: NTPv4,
		Mode:          MODE_SERVER,
		Stratum:       1,
		Poll:          3,
		Precision:     -29,
		ReferenceId:   [4]byte{'A', 'C', 'T', 'S'},
		ReferenceTimestamp: Timestamp{
			Seconds: 0xd90b4fbc, Fraction: 0x94e375b0},
		OriginTimestamp: Timestamp{
			Seconds: 0xd90b4fca, Fraction: 0x77ce2d4e},
		ReceiveTimestamp: Timestamp{
			Seconds: 0xd90b4fca, Fraction: 0x7e403121},
		TransmitTimestamp: Timestamp{
			Seconds: 0xd90b4fca, Fraction: 0x7e41298d},
	}, []byte{
		0x24, 0x01, 0x03, 0xe3, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x41, 0x43, 0x54, 0x53,
		0xd9, 0x0b, 0x4f, 0xbc, 0x94, 0xe3, 0x75, 0xb0,
		0xd9, 0x0b, 0x4f, 0xca, 0x77, 0xce, 0x2d, 0x4e,
		0xd9, 0x0b, 0x4f, 0xca, 0x7e, 0x40, 0x31, 0x21,
		0xd9, 0x0b, 0x4f, 0xca, 0x7e, 0x41, 0x29, 0x8d,
	}},
}

func TestHeaderMarshal(t *testing.T) {
	for i, d := range testData {
		b, err := d.Header.MarshalBinary()
		if err != nil {
			t.Errorf("testData[%d] marshal failed: %v", i, err)
			continue
		}
		if diff := pretty.Compare(d.Raw, b); diff != "" {
			t.Errorf("testData[%d] failed: %v", i, diff)
		}
	}
}

func BenchmarkHeaderMarshal(b *testing.B) {
	h := testData[0].Header
	for i := 0; i < b.N; i++ {
		if _, err := h.MarshalBinary(); err != nil {
			b.Error(err)
		}
	}
}

func TestHeaderUnmarshal(t *testing.T) {
	for i, d := range testData {
		if len(d.Raw) != headerSize {
			t.Errorf("testData[%d].Raw is invalid", i)
		}
		h := Header{}
		if err := h.UnmarshalBinary(d.Raw); err != nil {
			t.Errorf("testData[%d] unmarshal binary Raw failed: %v", i, err)
		}
		if diff := pretty.Compare(d.Header, h); diff != "" {
			t.Errorf("testData[%d] failed: %v", i, diff)
		}
	}
}

func BenchmarkHeaderUnmarshal(b *testing.B) {
	h := Header{}
	d := testData[0].Raw
	for i := 0; i < b.N; i++ {
		if err := h.UnmarshalBinary(d); err != nil {
			b.Error(err)
		}
	}
}

func TestNow(t *testing.T) {
	timestamp := Now()
	nsec := (float64(timestamp.Fraction) / (1 << 32)) * 1e9
	ntpTime := time.Unix(int64(timestamp.Seconds-JAN_1970), int64(nsec))
	if d := time.Since(ntpTime); d > time.Second {
		t.Errorf("Now() off by %s", d)
	}
}

func BenchmarkNow(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Now()
	}
}

func TestPrecision(t *testing.T) {
	precision := Precision()
	if precision != -20 {
		t.Errorf("Precision() reported %d", precision)
	}
}
