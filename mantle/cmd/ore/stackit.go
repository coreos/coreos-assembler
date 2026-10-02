// Copyright The Mantle Authors.
// SPDX-License-Identifier: Apache-2.0

package main

import "github.com/coreos/coreos-assembler/mantle/cmd/ore/stackit"

func init() {
	root.AddCommand(stackit.STACKIT)
}
