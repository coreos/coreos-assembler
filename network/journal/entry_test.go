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

package journal

import (
	"testing"
	"time"
)

func TestEntryRealtime(t *testing.T) {
	for _, testcase := range []struct {
		name   string
		entry  Entry
		expect time.Time
	}{{
		name:   "zero",
		entry:  Entry{},
		expect: time.Time{},
	}, {
		name: "has_source",
		entry: Entry{
			FIELD_SOURCE_REALTIME_TIMESTAMP: []byte("1342540861416351"),
			FIELD_REALTIME_TIMESTAMP:        []byte("1342540861416409"),
		},
		expect: time.Unix(1342540861, 416351000),
	}, {
		name: "no_source",
		entry: Entry{
			FIELD_REALTIME_TIMESTAMP: []byte("1342540861416409"),
		},
		expect: time.Unix(1342540861, 416409000),
	}, {
		name: "invalid_source",
		entry: Entry{
			FIELD_SOURCE_REALTIME_TIMESTAMP: []byte("bob"),
			FIELD_REALTIME_TIMESTAMP:        []byte("1342540861416409"),
		},
		expect: time.Time{},
	}, {
		name: "invalid_both",
		entry: Entry{
			FIELD_REALTIME_TIMESTAMP: []byte("bob"),
		},
		expect: time.Time{},
	},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			result := testcase.entry.Realtime()
			if result != testcase.expect {
				t.Errorf("%s != %s", result, testcase.expect)
			}
		})
	}
}
