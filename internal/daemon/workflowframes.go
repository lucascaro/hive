// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package daemon

import (
	"context"
	"errors"

	"github.com/lucascaro/hive/internal/registry"
	"github.com/lucascaro/hive/internal/wire"
	"github.com/lucascaro/hive/internal/worktree"
)

// PREPARE_WORKTREE / COMMIT_WORKTREE (spec 528). Both are
// unrestricted like the other worktree verbs (a plugin runs with the
// user's reach) but absent from sessionModeFrames, and both run off the
// read loop: a prepare is several git subprocesses.

// workflowWorktreeError names a failed prepare/commit on the wire.
// generic is the code for a failure with no specific cause (git said
// no); the specific ones are the refusals a plugin can act on.
func workflowWorktreeError(err error, generic string) (code string) {
	switch {
	case errors.Is(err, worktree.ErrInvalidRunID):
		return wire.ErrCodeInvalidWorkflowRunID
	case errors.Is(err, worktree.ErrInvalidWorkflowBranch):
		return wire.ErrCodeInvalidWorkflowBranch
	case errors.Is(err, worktree.ErrNoCommit):
		return wire.ErrCodeInvalidBase
	case errors.Is(err, worktree.ErrIdentityMissing):
		return wire.ErrCodeGitIdentityMissing
	case errors.Is(err, worktree.ErrGitUnsupported):
		return wire.ErrCodeGitUnsupported
	case errors.Is(err, worktree.ErrDirty):
		return wire.ErrCodeWorktreeDirty
	case errors.Is(err, registry.ErrProjectNotFound):
		return "no_such_project"
	}
	return generic
}

func (d *Daemon) prepareWorktree(ctx context.Context, ops controlOps, payload []byte) {
	req, ok := decodeReq[wire.PrepareWorktreeReq](payload, ops.sendError)
	if !ok {
		return
	}
	d.runOp(func() {
		msg, err := d.reg.PrepareWorktree(ctx, req)
		if err != nil {
			ops.sendError(workflowWorktreeError(err, "prepare_worktree_failed"), err.Error())
			return
		}
		msg.ReqID = ops.reqID
		_ = ops.writeJSON(wire.FramePreparedWorktree, msg)
	})
}

func (d *Daemon) commitWorktree(ctx context.Context, ops controlOps, payload []byte) {
	req, ok := decodeReq[wire.CommitWorktreeReq](payload, ops.sendError)
	if !ok {
		return
	}
	d.runOp(func() {
		committed, head, err := d.reg.CommitWorktree(ctx, req)
		if err != nil {
			code := workflowWorktreeError(err, "commit_worktree_failed")
			if errors.Is(err, worktree.ErrNotManaged) {
				code = wire.ErrCodeWorktreeNotFound
			}
			ops.sendError(code, err.Error())
			return
		}
		ops.ack(wire.AckMsg{Committed: committed, Head: head})
	})
}
