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

	"github.com/coreos/mantle/Godeps/_workspace/src/github.com/coreos/pkg/capnslog"
)

var plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "network/ntp")

type Server struct {
	net.PacketConn
}

type ServerReq struct {
	Client   net.Addr
	Received Timestamp
	Packet   []byte
}

func NewServer(addr string) (*Server, error) {
	l, err := net.ListenPacket("udp", addr)
	if err != nil {
		return nil, err
	}

	return &Server{l}, nil
}

func (s Server) Serve() {
	plog.Infof("Started NTP server on %s", s.LocalAddr())

	for {
		req, err := s.Accept()
		if err != nil {
			plog.Errorf("NTP server failed: %v", err)
			return
		}
		go s.Respond(req)
	}
}

func (s Server) Accept() (*ServerReq, error) {
	pkt := make([]byte, 1024)
	n, client, err := s.ReadFrom(pkt)
	if err != nil {
		return nil, err
	}
	return &ServerReq{
		Client:   client,
		Received: Now(),
		Packet:   pkt[:n],
	}, nil
}

func (s Server) Respond(r *ServerReq) {
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

	now := Now()
	resp := Header{
		LeapIndicator:      LEAP_NONE,
		VersionNumber:      NTPv4,
		Mode:               MODE_SERVER,
		Poll:               6, // 64s, arbitrary...
		Stratum:            7, // Anything within [2,14] will work
		Precision:          Precision(),
		ReferenceTimestamp: now,
		OriginTimestamp:    recv.TransmitTimestamp,
		ReceiveTimestamp:   r.Received,
		TransmitTimestamp:  now,
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
