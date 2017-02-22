// Copyright 2017 CoreOS, Inc.
// Copyright 2015 The Go Authors.
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

package harness

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

// matcher sanitizes, uniques, and filters names of subtests and subbenchmarks.
type matcher struct {
	filter []string

	mu       sync.Mutex
	subNames map[string]int64
}

// TODO: fix test_main to avoid race and improve caching, also allowing to
// eliminate this Mutex.
var matchMutex sync.Mutex
var matchPat string
var matchRe *regexp.Regexp

func matchString(pat, str string) (result bool, err error) {
	if matchRe == nil || matchPat != pat {
		matchPat = pat
		matchRe, err = regexp.Compile(matchPat)
		if err != nil {
			return
		}
	}
	return matchRe.MatchString(str), nil
}

func newMatcher(patterns, name string) *matcher {
	var filter []string
	if patterns != "" {
		filter = splitRegexp(patterns)
		for i, s := range filter {
			filter[i] = rewrite(s)
		}
		// Verify filters before doing any processing.
		for i, s := range filter {
			if _, err := matchString(s, "non-empty"); err != nil {
				fmt.Fprintf(os.Stderr, "testing: invalid regexp for element %d of %s (%q): %s\n", i, name, s, err)
				os.Exit(1)
			}
		}
	}
	return &matcher{
		filter:   filter,
		subNames: map[string]int64{},
	}
}

func (m *matcher) fullName(c *H, subname string) (name string, ok bool) {
	name = subname

	m.mu.Lock()
	defer m.mu.Unlock()

	if c != nil && c.level > 0 {
		name = m.unique(c.name, rewrite(subname))
	}

	matchMutex.Lock()
	defer matchMutex.Unlock()

	// We check the full array of paths each time to allow for the case that
	// a pattern contains a '/'.
	for i, s := range strings.Split(name, "/") {
		if i >= len(m.filter) {
			break
		}
		if ok, _ := matchString(m.filter[i], s); !ok {
			return name, false
		}
	}
	return name, true
}

func splitRegexp(s string) []string {
	a := make([]string, 0, strings.Count(s, "/"))
	cs := 0
	cp := 0
	for i := 0; i < len(s); {
		switch s[i] {
		case '[':
			cs++
		case ']':
			if cs--; cs < 0 { // An unmatched ']' is legal.
				cs = 0
			}
		case '(':
			if cs == 0 {
				cp++
			}
		case ')':
			if cs == 0 {
				cp--
			}
		case '\\':
			i++
		case '/':
			if cs == 0 && cp == 0 {
				a = append(a, s[:i])
				s = s[i+1:]
				i = 0
				continue
			}
		}
		i++
	}
	return append(a, s)
}

// unique creates a unique name for the given parent and subname by affixing it
// with one ore more counts, if necessary.
func (m *matcher) unique(parent, subname string) string {
	name := fmt.Sprintf("%s/%s", parent, subname)
	empty := subname == ""
	for {
		next, exists := m.subNames[name]
		if !empty && !exists {
			m.subNames[name] = 1 // next count is 1
			return name
		}
		// Name was already used. We increment with the count and append a
		// string with the count.
		m.subNames[name] = next + 1

		// Add a count to guarantee uniqueness.
		name = fmt.Sprintf("%s#%02d", name, next)
		empty = false
	}
}

// rewrite rewrites a subname to having only printable characters and no white
// space.
func rewrite(s string) string {
	b := []byte{}
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			b = append(b, '_')
		case !strconv.IsPrint(r):
			s := strconv.QuoteRune(r)
			b = append(b, s[1:len(s)-1]...)
		default:
			b = append(b, string(r)...)
		}
	}
	return string(b)
}
