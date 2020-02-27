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

package ntp

import (
	"testing"
	"time"
)

func TestNewServer(t *testing.T) {
	s, err := NewServer(":0")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
}

func TestServerSetTime(t *testing.T) {
	start := time.Date(2013, time.December, 10, 19, 22, 35, 0, time.UTC)
	s := &Server{}
	s.SetTime(start)
	offset := time.Now().Add(s.offset).Sub(start)
	if offset < time.Duration(0) || offset > time.Second {
		t.Errorf("Server time off by %s, internal offest %s",
			offset, s.offset)
	}
	s.SetTime(time.Time{})
	if s.offset != time.Duration(0) {
		t.Errorf("Server offset not zero: %s", s.offset)
	}
}

func TestServerSetLeap(t *testing.T) {
	leap := time.Date(2012, time.July, 1, 0, 0, 0, 0, time.UTC)
	s := &Server{}
	s.SetLeapSecond(leap, LEAP_ADD)
	if s.leapTime != leap || s.leapType != LEAP_ADD {
		t.Errorf("Wrong leap: %s %s", s.leapTime, s.leapType)
	}
}

type update struct {
	now time.Duration
	off time.Duration
	li  LeapIndicator
}

func TestServerUpdate(t *testing.T) {
	leap := time.Date(2012, time.July, 1, 0, 0, 0, 0, time.UTC)
	s := &Server{}

	s.SetLeapSecond(leap, LEAP_ADD)
	for _, u := range []update{
		{-48 * time.Hour, 0, LEAP_NONE},
		{-25 * time.Hour, 0, LEAP_NONE},
		{-24 * time.Hour, 0, LEAP_ADD},
		{-23 * time.Hour, 0, LEAP_ADD},
		{-time.Hour, 0, LEAP_ADD},
		{-time.Minute, 0, LEAP_ADD},
		{-time.Second, 0, LEAP_ADD},
		{-time.Millisecond, 0, LEAP_ADD},
		{-time.Microsecond, 0, LEAP_ADD},
		{-time.Nanosecond, 0, LEAP_ADD},
		{0, -time.Second, LEAP_NONE},
		{time.Nanosecond, -time.Second, LEAP_NONE},
		{time.Microsecond, -time.Second, LEAP_NONE},
		{time.Millisecond, -time.Second, LEAP_NONE},
		{time.Second, -time.Second, LEAP_NONE},
		{time.Minute, -time.Second, LEAP_NONE},
		{time.Hour, -time.Second, LEAP_NONE},
	} {
		now := leap.Add(u.now)
		off, li := s.UpdateOffset(leap.Add(u.now))
		if off != u.off || li != u.li {
			t.Errorf("Wrong update at %s: %s!=%s %s!=%s",
				now, u.off, off, u.li, li)
		}
	}

	s = &Server{}
	s.SetLeapSecond(leap, LEAP_SUB)
	for _, u := range []update{
		{-48 * time.Hour, 0, LEAP_NONE},
		{-25 * time.Hour, 0, LEAP_NONE},
		{-24 * time.Hour, 0, LEAP_SUB},
		{-23 * time.Hour, 0, LEAP_SUB},
		{-time.Hour, 0, LEAP_SUB},
		{-time.Minute, 0, LEAP_SUB},
		{-time.Second - time.Second, 0, LEAP_SUB},
		{-time.Second - time.Millisecond, 0, LEAP_SUB},
		{-time.Second - time.Microsecond, 0, LEAP_SUB},
		{-time.Second - time.Nanosecond, 0, LEAP_SUB},
		{-time.Second, time.Second, LEAP_NONE},
		{time.Nanosecond, time.Second, LEAP_NONE},
		{time.Microsecond, time.Second, LEAP_NONE},
		{time.Millisecond, time.Second, LEAP_NONE},
		{time.Second, time.Second, LEAP_NONE},
		{time.Minute, time.Second, LEAP_NONE},
		{time.Hour, time.Second, LEAP_NONE},
	} {
		now := leap.Add(u.now)
		off, li := s.UpdateOffset(leap.Add(u.now))
		if off != u.off || li != u.li {
			t.Errorf("Wrong update at %s: %s!=%s %s!=%s",
				now, u.off, off, u.li, li)
		}
	}
}
