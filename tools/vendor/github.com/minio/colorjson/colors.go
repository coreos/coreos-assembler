// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the https://golang.org/LICENSE file.

package colorjson

import (
	"github.com/fatih/color"
	"github.com/minio/pkg/console"
)

const (
	// FgDarkGray is the shell color code for dark gray. Needs to be followed by
	// FgBlack to render dark gray
	FgDarkGray = 1
	jsonString = "jsonGreen"
	jsonBool   = "jsonRed"
	jsonNum    = "jsonRed"
	jsonKey    = "jsonBoldBlue"
	jsonNull   = "jsonBoldDarkGray"
)

func init() {
	console.SetColor(jsonString, color.New(color.FgGreen))
	console.SetColor(jsonBool, color.New(color.FgRed))
	console.SetColor(jsonNum, color.New(color.FgRed))
	console.SetColor(jsonKey, color.New(color.FgBlue, color.Bold))
	console.SetColor(jsonNull, color.New(FgDarkGray, color.FgBlack, color.Bold))
}
