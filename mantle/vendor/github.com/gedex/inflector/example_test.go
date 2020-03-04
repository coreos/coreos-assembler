// Copyright 2013 Akeda Bagus <admin@gedex.web.id>. All rights reserved.
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
package inflector_test

import (
	"fmt"

	"github.com/gedex/inflector"
)

func ExampleSingularize() {
	fmt.Println(inflector.Singularize("People"))
	// Output:
	// Person
}

func ExamplePluralize() {
	fmt.Println(inflector.Pluralize("octopus"))
	// Output:
	// octopuses
}
