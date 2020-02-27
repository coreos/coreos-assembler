// Copyright 2017 CoreOS, Inc.
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

package packet

import (
	"bytes"
	"os"

	"golang.org/x/crypto/ssh"
)

type console struct {
	pc   *cluster
	f    *os.File
	buf  bytes.Buffer
	done chan interface{}
}

func (c *console) SSHClient(ip, user string) (*ssh.Client, error) {
	return c.pc.UserSSHClient(ip, user)
}

func (c *console) Write(p []byte) (int, error) {
	c.buf.Write(p)
	return c.f.Write(p)
}

func (c *console) Close() error {
	close(c.done)
	return c.f.Close()
}

func (c *console) Output() string {
	<-c.done
	return c.buf.String()
}
