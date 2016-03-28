// Copyright 2016 CoreOS, Inc.
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

package index

import (
	"fmt"

	"github.com/coreos/mantle/Godeps/_workspace/src/google.golang.org/api/googleapi"
)

type Error struct {
	Op     string
	Bucket string
	Name   string
	Err    error
}

func (e *Error) Error() string {
	if e.Bucket == "" {
		return fmt.Sprintf("%s %v", e.Op, e.Err)
	}
	return fmt.Sprintf("%s gs://%s/%s %v", e.Op, e.Bucket, e.Name, e.Err)
}

func wrapError(op, bucket, name string, e error) error {
	if _, ok := e.(*googleapi.Error); ok {
		return &Error{Op: op, Bucket: bucket, Name: name, Err: e}
	}
	return e
}
