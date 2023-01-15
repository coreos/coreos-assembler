// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
// Licensed under the same terms as Go itself:
// https://github.com/golang/go/blob/master/LICENSE

package bufnet

import (
	"errors"
	"net"
	"time"

	"github.com/coreos/coreos-assembler/mantle/lang/bufpipe"
)

// Pipe creates a synchronous, in-memory, full duplex
// network connection with unlimited buffering.
// Both ends implement the Conn interface.
func Pipe() (net.Conn, net.Conn) {
	r1, w1 := bufpipe.Pipe()
	r2, w2 := bufpipe.Pipe()

	return &pipe{r1, w2}, &pipe{r2, w1}
}

// FixedPipe creates a synchronous, in-memory, full duplex
// network connection with fixed-size buffers that have
// at least the specified size.
// Both ends implement the Conn interface.
func FixedPipe(size int) (net.Conn, net.Conn) {
	r1, w1 := bufpipe.FixedPipe(size)
	r2, w2 := bufpipe.FixedPipe(size)

	return &pipe{r1, w2}, &pipe{r2, w1}
}

type pipe struct {
	*bufpipe.PipeReader
	*bufpipe.PipeWriter
}

type pipeAddr int

func (pipeAddr) Network() string {
	return "pipe"
}

func (pipeAddr) String() string {
	return "pipe"
}

func (p *pipe) Close() error {
	err := p.PipeReader.Close()
	err1 := p.PipeWriter.Close()
	if err == nil {
		err = err1
	}
	return err
}

func (p *pipe) LocalAddr() net.Addr {
	return pipeAddr(0)
}

func (p *pipe) RemoteAddr() net.Addr {
	return pipeAddr(0)
}

func (p *pipe) SetDeadline(t time.Time) error {
	return &net.OpError{Op: "set", Net: "pipe", Source: nil, Addr: nil, Err: errors.New("deadline not supported")}
}

func (p *pipe) SetReadDeadline(t time.Time) error {
	return &net.OpError{Op: "set", Net: "pipe", Source: nil, Addr: nil, Err: errors.New("deadline not supported")}
}

func (p *pipe) SetWriteDeadline(t time.Time) error {
	return &net.OpError{Op: "set", Net: "pipe", Source: nil, Addr: nil, Err: errors.New("deadline not supported")}
}
