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

package neterror

import (
	"net"
)

// IsClosed detects if an error is due to a closed network connection,
// working around bug https://github.com/golang/go/issues/4373
func IsClosed(err error) bool {
	if err == nil {
		return false
	}
	if operr, ok := err.(*net.OpError); ok {
		err = operr.Err
	}
	// cry softly
	return err.Error() == "use of closed network connection"
}
