// Copyright (c) 2020 Andreas Auernhammer. All rights reserved.
// Use of this source code is governed by a license that can be
// found in the LICENSE file.

// +build !go1.14 !ppc64le

package sioutil

// An asm implementation of AES-GCM for ppc64le is not available
// before Go 1.14.

const ppcHasAES = false
