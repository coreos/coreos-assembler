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

package destructor

import (
	"io"

	"github.com/coreos/pkg/capnslog"
)

var (
	plog = capnslog.NewPackageLogger("github.com/coreos/mantle", "lang/destructor")
)

// Destructor is a common interface for objects that need to be cleaned up.
type Destructor interface {
	Destroy()
}

// CloseDestructor wraps any Closer to provide the Destructor interface.
type CloserDestructor struct {
	io.Closer
}

func (c CloserDestructor) Destroy() {
	if err := c.Close(); err != nil {
		plog.Errorf("Close() returned error: %v", err)
	}
}

// MultiDestructor wraps multiple Destructors for easy cleanup.
type MultiDestructor []Destructor

func (m MultiDestructor) Destroy() {
	for _, d := range m {
		d.Destroy()
	}
}

func (m *MultiDestructor) AddCloser(closer io.Closer) {
	m.AddDestructor(CloserDestructor{closer})
}

func (m *MultiDestructor) AddDestructor(destructor Destructor) {
	*m = append(*m, destructor)
}
