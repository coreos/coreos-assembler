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
	mux.Handle("/v1/update/", &OmahaHandler{s})

	s.srv.Handler = mux

	return s, nil
}

type Server struct {
	UpdaterStub

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

func (s *Server) Destroy() error {
	s.l.Close()

	s.reqChanLock.Lock()
	if s.reqChan != nil {
		close(s.reqChan)
		s.reqChan = nil
	}
	s.reqChanLock.Unlock()
	return nil
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

func (s *Server) CheckApp(req *Request, app *AppRequest) error {
	s.reqChanLock.Lock()
	if s.reqChan != nil {
		s.reqChan <- req
	}
	s.reqChanLock.Unlock()
	return nil
}

func (s *Server) Addr() net.Addr {
	return s.l.Addr()
}
