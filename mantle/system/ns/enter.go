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

package ns

import (
	"runtime"

	"github.com/vishvananda/netns"
)

// NsEnter locks the current goroutine the OS thread and switches to a
// new network namespace. The returned function must be called in order
// to restore the previous state and unlock the thread.
func Enter(ns netns.NsHandle) (func() error, error) {
	runtime.LockOSThread()

	origns, err := netns.Get()
	if err != nil {
		runtime.UnlockOSThread()
		return nil, err
	}

	err = netns.Set(ns)
	if err != nil {
		origns.Close()
		runtime.UnlockOSThread()
		return nil, err
	}

	return func() error {
		defer runtime.UnlockOSThread()
		defer origns.Close()
		if err := netns.Set(origns); err != nil {
			return err
		}
		return nil
	}, nil
}

// NsCreate returns a handle to a new network namespace.
// NsEnter must be used to safely enter and exit the new namespace.
func Create() (netns.NsHandle, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	origns, err := netns.Get()
	if err != nil {
		return netns.None(), err
	}
	defer origns.Close()

	newns, err := netns.New()
	if err != nil {
		return netns.None(), err
	}

	if err := netns.Set(origns); err != nil {
		newns.Close()
		return netns.None(), err
	}

	return newns, nil
}
