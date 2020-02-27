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
	"net"
	"sync"
	"time"

	"github.com/coreos/pkg/capnslog"

	"github.com/coreos/mantle/network/neterror"
)

var plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "network/ntp")

// BUG(marineam): Since our clock source is UTC instead of TAI or some type of
// monotonic clock, trying to use this server during a real leap second will
// lead to incorrect results. Timekeeping sucks.

// A simple NTP server intended for testing. It can serve time at some offset
// from the real time and adjust for a single leap second.
type Server struct {
	net.PacketConn
	mu       sync.Mutex    // protects offset, leapTime, and leapType.
	offset   time.Duration // see SetTime
	leapTime time.Time     // see SetLeapSecond
	leapType LeapIndicator
}

type ServerReq struct {
	Client   net.Addr
	Received time.Time
	Packet   []byte
}

// Create a NTP server that listens on the given address.
func NewServer(addr string) (*Server, error) {
	l, err := net.ListenPacket("udp", addr)
	if err != nil {
		return nil, err
	}

	return &Server{PacketConn: l}, nil
}

// Adjust the internal time offset to begin serving based on the given time.
func (s *Server) SetTime(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if now.IsZero() {
		s.offset = time.Duration(0)
	} else {
		s.offset = -time.Since(now)
	}
}

// Must be exactly midnight on the first day of the month. This is the first
// time that is always valid after the leap has occurred for both adding and
// removing a second. This is the same way leap seconds are officially listed.
// https://www.ietf.org/timezones/data/leap-seconds.list
func (s *Server) SetLeapSecond(second time.Time, direction LeapIndicator) {
	second = second.UTC()
	if (second.IsZero() && direction != LEAP_NONE) ||
		(second.Truncate(24*time.Hour) != second) ||
		(second.Day() != 1) {
		panic("Invalid leap second.")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.leapTime = second
	s.leapType = direction
}

// Get the current offset between real time and the server's time, adjusting
// for a leap second as needed. now is real time, not server time.
func (s *Server) UpdateOffset(now time.Time) (time.Duration, LeapIndicator) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.leapTime.IsZero() || s.leapType == LEAP_NONE {
		return s.offset, LEAP_NONE
	}

	now = now.Add(s.offset)
	if now.Add(24 * time.Hour).Before(s.leapTime) {
		return s.offset, LEAP_NONE
	}

	if s.leapType == LEAP_ADD && !now.Before(s.leapTime) {
		plog.Infof("Inserting leap second at %s", s.leapTime)
		s.offset -= time.Second
		s.leapTime = time.Time{}
		s.leapType = LEAP_NONE

	} else if s.leapType == LEAP_SUB &&
		!now.Before(s.leapTime.Add(-time.Second)) {

		plog.Infof("Skipping leap second at %s", s.leapTime)
		s.offset += time.Second
		s.leapTime = time.Time{}
		s.leapType = LEAP_NONE
	}

	return s.offset, s.leapType
}

// Serve NTP requests forever.
func (s *Server) Serve() {
	plog.Infof("Started NTP server on %s", s.LocalAddr())

	for {
		req, err := s.Accept()
		if neterror.IsClosed(err) {
			// gracefully quit
			return
		} else if err != nil {
			plog.Errorf("NTP server failed: %v", err)
			return
		}
		go s.Respond(req)
	}
}

// Accept a single NTP request.
func (s *Server) Accept() (*ServerReq, error) {
	pkt := make([]byte, 1024)
	n, client, err := s.ReadFrom(pkt)
	if err != nil {
		return nil, err
	}
	return &ServerReq{
		Client:   client,
		Received: time.Now(),
		Packet:   pkt[:n],
	}, nil
}

// Respond to a single NTP request.
func (s *Server) Respond(r *ServerReq) {
	if len(r.Packet) == cap(r.Packet) {
		plog.Errorf("Ignoring huge NTP packet from %s", r.Client)
		return
	}

	recv := Header{}
	if err := recv.UnmarshalBinary(r.Packet); err != nil {
		plog.Errorf("Invalid NTP packet from %s: %v", r.Client, err)
		return
	}

	if recv.VersionNumber != NTPv4 {
		plog.Errorf("Invalid NTP version from %s: %d", r.Client, recv.VersionNumber)
		return
	}

	if recv.Mode != MODE_CLIENT {
		plog.Errorf("Invalid NTP mode from %s: %d", r.Client, recv.Mode)
		return
	}

	plog.Infof("Recieved NTP request from %s", r.Client)

	// BUG(marineam): We doesn't account for the possibility of
	// UpdateOffset behaving differently for the transmit time instead of
	// the received time. No idea what the correct behavior is.
	offset, leap := s.UpdateOffset(r.Received)
	received := NewTimestamp(r.Received.Add(offset))
	transmit := NewTimestamp(time.Now().Add(offset))

	resp := Header{
		LeapIndicator:      leap,
		VersionNumber:      NTPv4,
		Mode:               MODE_SERVER,
		Poll:               6, // 64s, arbitrary...
		Stratum:            7, // Anything within [2,14] will work
		Precision:          Precision(),
		ReferenceTimestamp: received,
		OriginTimestamp:    recv.TransmitTimestamp,
		ReceiveTimestamp:   received,
		TransmitTimestamp:  transmit,
	}

	pkt, err := resp.MarshalBinary()
	if err != nil {
		plog.Errorf("Creating NTP packet failed: %v", err)
		return
	}

	_, err = s.WriteTo(pkt, r.Client)
	if err != nil {
		plog.Errorf("Error sending NTP packet to %s: %v", r.Client, err)
		return
	}
}
