package main

/*
	Ensure that schematyper survives `go mod tidy`

	This file is a COSA specific addition.
*/

import (
	_ "github.com/alecthomas/template"
	_ "github.com/gedex/inflector"
	_ "github.com/idubinskiy/schematyper/stringset"
	_ "gopkg.in/alecthomas/kingpin.v2"
)
