package stringset

import (
	"math/rand"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNew(t *testing.T) {
	Convey("Given some strings", t, func() {
		arg1, arg2, arg3 := "one", "two", "three"

		Convey("When we initialize a StringSet using New", func() {
			s := New(arg1, arg2, arg3)

			Convey("Then it should have those strings as values", func() {
				So(s.Has(arg1), ShouldBeTrue)
				So(s.Has(arg2), ShouldBeTrue)
				So(s.Has(arg2), ShouldBeTrue)
			})
		})
	})
}

func TestFromSlice(t *testing.T) {
	Convey("When we initialize a StringSet without values", t, func() {
		s := New()

		Convey("Then it should be empty", func() {
			So(s.Len(), ShouldEqual, 0)
		})
	})

	Convey("Given a slice of strings", t, func() {
		sl := []string{"one", "two", "three"}

		Convey("When we initialize a StringSet using FromSlice", func() {
			s, err := FromSlice(sl)

			Convey("Then it should have those strings as values", func() {
				for _, val := range sl {
					So(s.Has(val), ShouldBeTrue)
				}
			})

			Convey("Then there should be no error", func() {
				So(err, ShouldBeNil)
			})
		})
	})

	Convey("Given a non-string slice", t, func() {
		sl := []int{1, 2, 3}

		Convey("When we initialize a StringSet using FromSlice", func() {
			_, err := FromSlice(sl)

			Convey("Then there should be an error", func() {
				So(err, ShouldEqual, ErrInvalidType)
			})
		})
	})

	Convey("Given a value that is not a slice", t, func() {
		sl := 0

		Convey("When we initialize a StringSet using FromSlice", func() {
			_, err := FromSlice(sl)

			Convey("Then there should be an error", func() {
				So(err, ShouldEqual, ErrInvalidType)
			})
		})
	})
}

func TestFromMapVals(t *testing.T) {
	Convey("Given a map with string values", t, func() {
		m := map[int]string{1: "one", 2: "two", 3: "three"}

		Convey("When we initialize a StringSet using FromMapVals", func() {
			s, err := FromMapVals(m)

			Convey("Then it should have those strings as values", func() {
				for _, val := range m {
					So(s.Has(val), ShouldBeTrue)
				}
			})

			Convey("Then there should be no error", func() {
				So(err, ShouldBeNil)
			})
		})
	})

	Convey("Given a map with non-string values", t, func() {
		m := map[string]int{"one": 1, "two": 2, "three": 3}

		Convey("When we initialize a StringSet using FromMapVals", func() {
			_, err := FromMapVals(m)

			Convey("Then there should be an error", func() {
				So(err, ShouldEqual, ErrInvalidType)
			})
		})
	})

	Convey("Given a value that is not a map", t, func() {
		m := 0

		Convey("When we initialize a StringSet using FromMapVals", func() {
			_, err := FromMapVals(m)

			Convey("Then there should be an error", func() {
				So(err, ShouldEqual, ErrInvalidType)
			})
		})
	})
}

func TestFromMapKeys(t *testing.T) {
	Convey("Given a map with string keys", t, func() {
		m := map[string]int{"one": 1, "two": 2, "three": 3}

		Convey("When we initialize a StringSet using FromMapKeys", func() {
			s, err := FromMapKeys(m)

			Convey("Then it should have those strings as values", func() {
				for val := range m {
					So(s.Has(val), ShouldBeTrue)
				}
			})

			Convey("Then there should be no error", func() {
				So(err, ShouldBeNil)
			})
		})
	})

	Convey("Given a map with non-string keys", t, func() {
		m := map[int]string{1: "one", 2: "two", 3: "three"}

		Convey("When we initialize a StringSet using FromMapKeys", func() {
			_, err := FromMapKeys(m)

			Convey("Then there should be an error", func() {
				So(err, ShouldEqual, ErrInvalidType)
			})
		})
	})

	Convey("Given a value that is not a map", t, func() {
		m := 0

		Convey("When we initialize a StringSet using FromMapVals", func() {
			_, err := FromMapKeys(m)

			Convey("Then there should be an error", func() {
				So(err, ShouldEqual, ErrInvalidType)
			})
		})
	})
}

func TestAdd(t *testing.T) {
	Convey("Given an empty StringSet", t, func() {
		s := New()

		Convey("When we add a value", func() {
			val := "foo"
			s.Add(val)

			Convey("Then the set should have that value", func() {
				So(s.Has(val), ShouldBeTrue)
			})
		})
	})
}

func TestRemove(t *testing.T) {
	Convey("Given a StringSet with a value", t, func() {
		val := "foo"
		s := New(val)

		Convey("When we delete the value", func() {
			s.Remove(val)

			Convey("Then the set should no longer have that value", func() {
				So(s.Has(val), ShouldBeFalse)
			})
		})
	})
}

func TestHas(t *testing.T) {
	Convey("Given a StringSet with a single value", t, func() {
		val := "foo"
		s := New(val)

		Convey("Then it should have that value", func() {
			So(s.Has(val), ShouldBeTrue)
		})

		Convey("Then it should not have other values", func() {
			So(s.Has("bar"), ShouldBeFalse)
		})
	})
}

func TestSlice(t *testing.T) {
	Convey("Given a StringSet with some values", t, func() {
		vals := []string{"foo", "bar", "baz"}
		s, _ := FromSlice(vals)
		// add some duplicates
		for i := 0; i < 30; i++ {
			s.Add(vals[rand.Intn(len(vals))])
		}

		Convey("When we extract the values as a slice", func() {
			sl := s.Slice()

			Convey("Then the values should be in the slice", func() {
				for _, val := range vals {
					So(sl, ShouldContain, val)
				}
			})

			Convey("Then there should be no duplicates", func() {
				So(len(sl), ShouldEqual, len(vals))
			})
		})
	})
}

func TestSorted(t *testing.T) {
	Convey("Given a StringSet with some values", t, func() {
		sorted := []string{"bar", "baz", "foo", "qux", "thud"}
		s, _ := FromSlice(sorted)
		// add some duplicates
		for i := 0; i < 30; i++ {
			s.Add(sorted[rand.Intn(len(sorted))])
		}

		Convey("When we extract the values as a sorted slice", func() {
			sl := s.Sorted()

			Convey("Then the values should be in the slice in alphabetical order", func() {
				So(sl, ShouldResemble, sorted)
			})

			Convey("Then there should be no duplicates", func() {
				So(len(sl), ShouldEqual, len(sorted))
			})
		})
	})
}

func TestString(t *testing.T) {
	Convey("Given a StringSet with some values", t, func() {
		vals := []string{"foo", "bar", "baz"}
		s, _ := FromSlice(vals)

		Convey("When we get its string representation", func() {
			str := s.String()

			Convey("Then it should have all the values", func() {
				for _, val := range vals {
					So(str, ShouldContainSubstring, val)
				}
			})

			Convey("Then it should be wrapped in parentheses", func() {
				So(str, ShouldStartWith, "(")
				So(str, ShouldEndWith, ")")
			})

			Convey("Then there should not be a trailing space", func() {
				So(str, ShouldNotContainSubstring, " )")
			})
		})
	})

	Convey("Given an empty StringSet", t, func() {
		s := New()

		Convey("When we get its string representation", func() {
			str := s.String()

			Convey("Then it should be '()'", func() {
				So(str, ShouldEqual, "()")
			})
		})
	})
}

func TestEquals(t *testing.T) {
	Convey("Given an empty StringSet", t, func() {
		s := New()

		Convey("Then it should be equal to itself", func() {
			So(s.Equals(s), ShouldBeTrue)
		})
	})

	Convey("Given a non-empty StringSet", t, func() {
		s := New("foo", "bar", "baz")

		Convey("Then it should be equal to itself", func() {
			So(s.Equals(s), ShouldBeTrue)
		})
	})

	Convey("Given two StringSets initialized with the same values", t, func() {
		vals := []string{"foo", "bar", "baz"}
		s1, _ := FromSlice(vals)
		// add some duplicates
		for i := 0; i < 30; i++ {
			s1.Add(vals[rand.Intn(len(vals))])
		}
		s2, _ := FromSlice(vals)
		// add some duplicates
		for i := 0; i < 30; i++ {
			s2.Add(vals[rand.Intn(len(vals))])
		}

		Convey("Then they should be equal", func() {
			So(s1.Equals(s2), ShouldBeTrue)
			So(s2.Equals(s1), ShouldBeTrue)
		})
	})

	Convey("Given two StringSets with different values", t, func() {
		s1 := New("foo")
		s2 := New("bar")

		Convey("Then they should not be equal", func() {
			So(s1.Equals(s2), ShouldBeFalse)
			So(s2.Equals(s1), ShouldBeFalse)
		})
	})

	Convey("Given two StringSets with different lengths", t, func() {
		s1 := New("foo")
		s2 := New("foo", "bar")

		Convey("Then they should not be equal", func() {
			So(s1.Equals(s2), ShouldBeFalse)
			So(s2.Equals(s1), ShouldBeFalse)
		})
	})
}
