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

package omaha

import (
	"encoding/xml"
	"net"
	"net/http"
	"sync"
)

func NewServer(addr string) (*Server, error) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	srv := &http.Server{
		Addr: addr,
	}

	s := &Server{
		l:   l,
		srv: srv,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/update/", s.UpdateHandler)

	s.srv.Handler = mux

	return s, nil
}

type Server struct {
	l   net.Listener
	srv *http.Server

	reqChanLock sync.Mutex
	reqChan     chan *Request
}

func (s *Server) Serve() error {
	err := s.srv.Serve(s.l)
	if err != nil {
		netErr, ok := err.(*net.OpError)
		if ok {
			if netErr.Err.Error() == "use of closed network connection" {
				return nil
			}
		}

		return err
	}

	return nil
}

func (s *Server) Stop() {
	s.l.Close()

	s.reqChanLock.Lock()
	if s.reqChan != nil {
		close(s.reqChan)
		s.reqChan = nil
	}
	s.reqChanLock.Unlock()
}

// RequestChan returns a channel that will receive decoded omaha requests.
//
// XXX: if called more than once per Server instance, the receivers will miss
// requests.
func (s *Server) RequestChan() <-chan *Request {
	s.reqChanLock.Lock()
	if s.reqChan == nil {
		s.reqChan = make(chan *Request)
	}
	s.reqChanLock.Unlock()
	return s.reqChan
}

func (s *Server) UpdateHandler(rw http.ResponseWriter, r *http.Request) {
	req := Request{}
	err := xml.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(rw, "malformed omaha request: "+err.Error(), http.StatusBadRequest)
		return
	}

	s.reqChanLock.Lock()
	if s.reqChan != nil {
		s.reqChan <- &req
	}
	s.reqChanLock.Unlock()

	resp := NewResponse()
	app := resp.AddApp("e96281a6-d1af-4bde-9a0a-97b76e56dc57", "ok")
	app.AddUpdateCheck(NoUpdate)

	enc := xml.NewEncoder(rw)
	enc.Indent("", "\t")

	err = enc.Encode(resp)
	if err != nil {
		http.Error(rw, "failed to marshal omaha request: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

func (s *Server) Addr() net.Addr {
	return s.l.Addr()
}
