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
	"log"
	"net"
)

type Server struct {
	net.PacketConn
}

type ServerReq struct {
	Client   net.Addr
	Received Timestamp
	Packet   []byte
}

func (s Server) Serve() error {
	for {
		req, err := s.Accept()
		if err != nil {
			return err
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
		log.Printf("Ignoring huge NTP packet from %s", r.Client)
		return
	}

	recv := Header{}
	if err := recv.UnmarshalBinary(r.Packet); err != nil {
		log.Printf("Invalid NTP packet from %s: %v", r.Client, err)
		return
	}

	if recv.VersionNumber != NTPv4 {
		log.Printf("Invalid NTP version from %s: %d", r.Client, recv.VersionNumber)
		return
	}

	if recv.Mode != MODE_CLIENT {
		log.Printf("Invalid NTP mode from %s: %d", r.Client, recv.Mode)
		return
	}

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
		log.Printf("Creating NTP packet failed: %v", err)
		return
	}

	_, err = s.WriteTo(pkt, r.Client)
	if err != nil {
		log.Printf("Error sending NTP packet to %s: %v", r.Client, err)
		return
	}
}
