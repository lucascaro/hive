// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package daemon

import (
	"os"
	"testing"

	"github.com/lucascaro/hive/internal/acp/acptest"
)

// The ACP tests re-execute this test binary as a fake ACP agent.
func TestMain(m *testing.M) {
	if acptest.IsAgent() {
		acptest.Main()
		return
	}
	os.Exit(m.Run())
}
