// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
// Licensed under the same terms as Go itself:
// https://github.com/golang/go/blob/master/LICENSE

// Pipe adapter to connect code expecting an io.Reader
// with code expecting an io.Writer.

package bufpipe

import (
	"bytes"
	"io"
	"sync"
)

// A pipe is the shared pipe structure underlying PipeReader and PipeWriter.
type pipe struct {
	rl    sync.Mutex // gates readers one at a time
	wl    sync.Mutex // gates writers one at a time
	l     sync.Mutex // protects remaining fields
	buf   pipeBuffer // data buffer
	rwait sync.Cond  // waiting reader
	wwait sync.Cond  // waiting writer
	rerr  error      // if reader closed, error to give writes
	werr  error      // if writer closed, error to give reads
}

type pipeBuffer interface {
	Len() int
	io.Writer
	io.Reader
}

func newPipe(buf pipeBuffer) *pipe {
	p := &pipe{buf: buf}
	p.rwait.L = &p.l
	p.wwait.L = &p.l
	return p
}

func (p *pipe) read(b []byte) (n int, err error) {
	// One reader at a time.
	p.rl.Lock()
	defer p.rl.Unlock()

	p.l.Lock()
	defer p.l.Unlock()
	for {
		if p.rerr != nil {
			return 0, io.ErrClosedPipe
		}
		if p.buf.Len() > 0 {
			break
		}
		if p.werr != nil {
			return 0, p.werr
		}
		p.rwait.Wait()
	}
	n, err = p.buf.Read(b)
	p.wwait.Signal()
	return
}

var zero [0]byte

func (p *pipe) write(b []byte) (n int, err error) {
	// pipe uses nil to mean not available
	if b == nil {
		b = zero[:]
	}

	// One writer at a time.
	p.wl.Lock()
	defer p.wl.Unlock()

	p.l.Lock()
	defer p.l.Unlock()
	for {
		if p.werr != nil {
			err = io.ErrClosedPipe
			break
		}
		if p.rerr != nil {
			err = p.rerr
			break
		}
		nn, err := p.buf.Write(b[n:])
		p.rwait.Signal()
		n += nn
		if err != errWriteFull {
			break
		}
		p.wwait.Wait()
	}
	return
}

func (p *pipe) rclose(err error) {
	if err == nil {
		err = io.ErrClosedPipe
	}
	p.l.Lock()
	defer p.l.Unlock()
	p.rerr = err
	p.rwait.Signal()
	p.wwait.Signal()
}

func (p *pipe) wclose(err error) {
	if err == nil {
		err = io.EOF
	}
	p.l.Lock()
	defer p.l.Unlock()
	p.werr = err
	p.rwait.Signal()
	p.wwait.Signal()
}

// A PipeReader is the read half of a pipe.
type PipeReader struct {
	p *pipe
}

// Read implements the standard Read interface:
// it reads data from the pipe, blocking until a writer
// arrives or the write end is closed.
// Closing the write end does not prevent reading buffered data.
// If the write end is closed with an error, that error is
// returned as err; otherwise err is io.EOF.
func (r *PipeReader) Read(data []byte) (n int, err error) {
	return r.p.read(data)
}

// Close closes the reader; subsequent writes to the
// write half of the pipe will return the error io.ErrClosedPipe.
func (r *PipeReader) Close() error {
	return r.CloseWithError(nil)
}

// CloseWithError closes the reader; subsequent writes
// to the write half of the pipe will return the error err.
func (r *PipeReader) CloseWithError(err error) error {
	r.p.rclose(err)
	return nil
}

// A PipeWriter is the write half of a pipe.
type PipeWriter struct {
	p *pipe
}

// Write implements the standard Write interface:
// it writes data to the pipe, returning once the data is
// buffered or the read end is closed.
// When using a FixedPipe, Write may block until one
// or more readers have consumed some of the data.
// If the read end is closed with an error, that err is
// returned as err; otherwise err is io.ErrClosedPipe.
func (w *PipeWriter) Write(data []byte) (n int, err error) {
	return w.p.write(data)
}

// Close closes the writer.
// Buffered data may still be read.
// Once the buffer is empty subsequent reads from the
// read half of the pipe will return no bytes and io.EOF.
func (w *PipeWriter) Close() error {
	return w.CloseWithError(nil)
}

// CloseWithError closes the writer.
// Buffered data may still be read.
// Once the buffer is empty subsequent reads from the
// read half of the pipe will return no bytes and the error err,
// or io.EOF if err is nil.
//
// CloseWithError always returns nil.
func (w *PipeWriter) CloseWithError(err error) error {
	w.p.wclose(err)
	return nil
}

// Pipe creates a synchronous in-memory pipe with an unlimited buffer.
// If the input size is unknown a FixedPipe may be preferable.
//
// Reads will block until data is written.
// Writes will never block.
//
// It is safe to call Read and Write in parallel with each other or with Close.
// Parallel calls to Read and parallel calls to Write are also safe:
// the individual calls will be gated sequentially.
func Pipe() (*PipeReader, *PipeWriter) {
	p := newPipe(&bytes.Buffer{})
	r := &PipeReader{p}
	w := &PipeWriter{p}
	return r, w
}

const minBufferSize = 16

// FixedPipe creates a synchronous in-memory pipe with a
// fixed-size buffer that has at least the specified size.
// It can be used to mimic a kernel provided fifo or socket
// which have an internal buffer and blocking I/O.
//
// Reads will block until data is written.
// Writes will block when the internal buffer is filled.
//
// It is safe to call Read and Write in parallel with each other or with Close.
// Parallel calls to Read and parallel calls to Write are also safe:
// the individual calls will be gated sequentially.
func FixedPipe(size int) (*PipeReader, *PipeWriter) {
	if size < minBufferSize {
		size = minBufferSize
	}
	p := newPipe(&fixedBuffer{buf: make([]byte, size)})
	r := &PipeReader{p}
	w := &PipeWriter{p}
	return r, w
}
