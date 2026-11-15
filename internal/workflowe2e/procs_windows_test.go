// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

//go:build windows

package workflowe2e

import "errors"

// The e2e tests skip on Windows (requireNode); these keep the package compiling there.

func alive(int) bool { return false }

func kill9(int) error { return errors.New("not supported on Windows") }
