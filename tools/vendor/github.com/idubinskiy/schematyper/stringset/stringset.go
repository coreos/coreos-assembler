package stringset

import (
	"bytes"
	"errors"
	"reflect"
	"sort"
)

// ErrInvalidType is returned by initialization functions expecting specific types.
var ErrInvalidType = errors.New("invalid type")

// StringSet is an unordered set of unique strings.
type StringSet map[string]struct{}

// New returns a new StringSet initialized with vals.
func New(vals ...string) StringSet {
	s := make(StringSet)
	for _, val := range vals {
		s[val] = struct{}{}
	}
	return s
}

// FromSlice returns a new StringSet initialized with s's values.
// It returns InvalidType if sl is not a slice of type string.
func FromSlice(sl interface{}) (StringSet, error) {
	v := reflect.ValueOf(sl)
	if v.Kind() != reflect.Slice {
		return nil, ErrInvalidType
	}
	if v.Type().Elem().Kind() != reflect.String {
		return nil, ErrInvalidType
	}

	s := make(StringSet, v.Len())
	for i := 0; i < v.Len(); i++ {
		s[v.Index(i).Interface().(string)] = struct{}{}
	}
	return s, nil
}

// FromMapKeys returns a new StringSet initialized with m's keys.
// It returns InvalidType if m is not a map or its keys are not type string.
func FromMapKeys(m interface{}) (StringSet, error) {
	v := reflect.ValueOf(m)
	if v.Kind() != reflect.Map {
		return nil, ErrInvalidType
	}
	if v.Type().Key().Kind() != reflect.String {
		return nil, ErrInvalidType
	}

	s := make(StringSet, v.Len())
	for _, val := range v.MapKeys() {
		s[val.Interface().(string)] = struct{}{}
	}
	return s, nil
}

// FromMapVals returns a new StringSet initialized with m's values.
// It returns InvalidType if m is not a map or its values are not type string.
func FromMapVals(m interface{}) (StringSet, error) {
	v := reflect.ValueOf(m)
	if v.Kind() != reflect.Map {
		return nil, ErrInvalidType
	}
	if v.Type().Elem().Kind() != reflect.String {
		return nil, ErrInvalidType
	}

	s := make(StringSet, v.Len())
	for _, val := range v.MapKeys() {
		s[v.MapIndex(val).Interface().(string)] = struct{}{}
	}
	return s, nil
}

// Add adds val to the set if it does not already exist.
func (s StringSet) Add(val string) {
	s[val] = struct{}{}
}

// Remove deletes val from the set if it exists.
func (s StringSet) Remove(val string) {
	delete(s, val)
}

// Has returns true if val is in the set.
func (s StringSet) Has(val string) bool {
	_, ok := s[val]
	return ok
}

// Equals returns true if all the values of s are in t and vice-versa.
// Order is not considered.
func (s StringSet) Equals(t StringSet) bool {
	if len(s) != len(t) {
		return false
	}

	for val := range s {
		if _, ok := t[val]; !ok {
			return false
		}
	}

	return true
}

func (s StringSet) String() string {
	b := bytes.Buffer{}
	b.WriteRune('(')

	for val := range s {
		b.WriteString(val)
		b.WriteRune(' ')
	}

	if size := b.Len(); size > 1 {
		b.Truncate(size - 1)
	}

	b.WriteRune(')')

	return b.String()
}

// Slice returns a slice with the values of the set in an arbitrary order.
func (s StringSet) Slice() []string {
	sl := make([]string, 0, len(s))
	for val := range s {
		sl = append(sl, val)
	}

	return sl
}

// Sorted returns a slice with the values of the set sorted.
func (s StringSet) Sorted() []string {
	sl := s.Slice()

	sort.StringSlice(sl).Sort()
	return sl
}

// Len returns the length of the set.
func (s StringSet) Len() int {
	return len(s)
}
