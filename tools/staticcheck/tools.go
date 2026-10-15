// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

//go:build tools

// Package tools pins how CI builds staticcheck. A separate module so the
// pin does not leak into hive's own go.mod.
//
// Why not plain `go install honnef.co/go/tools/cmd/staticcheck@2026.2.1`:
// that resolves golang.org/x/tools to the version staticcheck was released
// with, whose export-data reader stops at format 4. Go 1.27.2's compiler
// writes format 5, so staticcheck dies with "export data version 5 is
// greater than maximum supported version 4". Building it here, with a newer
// x/tools, reads both. Drop this module when a staticcheck release ships
// with x/tools >= v0.52.0.
package tools

import _ "honnef.co/go/tools/cmd/staticcheck"
