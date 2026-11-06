// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package plugin

import (
	"testing"

	"github.com/lucascaro/hive/internal/wire"
)

// Spec 528's frames, priced: LIST_ACP_AGENTS probes the login shell for
// every agent, so a plugin polling it pays fan-out rates; CANCEL_ACP is
// a cheap, idempotent write.
func TestFrameCost_WorkflowFrames(t *testing.T) {
	if got := FrameCost(wire.FrameListAcpAgents); got != costFanout {
		t.Errorf("LIST_ACP_AGENTS costs %v, want %v", got, float64(costFanout))
	}
	if got := FrameCost(wire.FrameCancelAcp); got != costCheap {
		t.Errorf("CANCEL_ACP costs %v, want %v", got, float64(costCheap))
	}
	// A prepare or commit runs child processes; a plugin message fans out
	// to every UI client.
	for ft, want := range map[wire.FrameType]float64{
		wire.FramePrepareWorktree: costExpense,
		wire.FrameCommitWorktree:  costExpense,
		wire.FramePluginMessage:   costFanout,
	} {
		if got := FrameCost(ft); got != want {
			t.Errorf("%s costs %v, want %v", ft, got, want)
		}
	}
}
